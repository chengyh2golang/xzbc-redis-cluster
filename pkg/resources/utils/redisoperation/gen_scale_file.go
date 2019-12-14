package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	scaleScriptFile = "/tmp/redis-trib-scale.sh"
	expectScriptFile = "/tmp/redis-trib-create.sh"
)

type reShardInfo struct {
	//执行reShard时，需要指定一个现有集群中的节点，格式：ip:6379
	//使用的是集群的第一个节点，它的域名类似：rediscluster01-0.rediscluster01.default.svc.cluster.local
	//基于这个域名去获取ip
	clusterInfoNode string

	//为了平衡每个master管理的slot的个数,16384/master个数
	//这个数字越大，对现有集群的影响越大，但是也越能做到slot在master间的平衡
	//总体slot是16384，这个版本硬编码，把基数降低到了4096，这样移动的slot会少，做reshard的时间也会减少
	slotCountByMasterMgmt int 
	nodeIDReceiving string //接收slot的node id，指定的是新加进来的master，新增4个节点，就是2个master
	//sourceNodeID string  //使用all，直接硬编码，从所有当前的master上都转移slot，也可以指定从某个或者某几个master
}

func main() {
	//首先获取CLUSTER_OP_TYPE这个系统环境变量
	//如果是"create"，就走创建集群的逻辑，如果是"scale"，就走扩容逻辑

	opType := os.Getenv("CLUSTER_OP_TYPE")
	
	if opType == "create" {
		//创建集群
		//获取系统环境变量，需要获取3个：
		// 集群的规模大小
		// redisClusterName实例的名字
		// namespace
		clusterSize := os.Getenv("CLUSTER_SIZE")
		clusterSizeInt,_ := strconv.Atoi(clusterSize)
		redisClusterName := os.Getenv("REDISCLUSTER_NAME")
		ns := os.Getenv("NAMESPACE")
		if len(clusterSize) == 0 || len(redisClusterName) == 0 || len(ns) == 0{
			panic(errors.New("读取环境变量出错"))
		}

		//判断所有redis cluster的节点是否都已开始监听6379端口
		envReady := checkRedisClusterNodeReady(clusterSizeInt, redisClusterName, ns)

		//如果集群节点redis服务都已经启动正常
		//准备使用reids-trib来初始化集群
		//使用redis-trib做初始化，必须要输入yes，所以使用expect来实现
		//这需要构建expect脚本
		if envReady {
			//定义expect文件路径

			//检查这个文件是否存在，如果不存在就创建
			exists, err := checkFileExists(expectScriptFile)
			if err != nil {
				log.Fatal(err)
			}
			if !exists {
				_, err := os.Create(expectScriptFile)
				if err != nil {
					panic(err)
				}
			}

			//打开文件
			file, err := os.OpenFile(expectScriptFile, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0666)
			if err != nil {
				fmt.Println("Fail to open file:", err)
			}

			defer func() {
				if err != nil {
					file.Close()
				} else {
					err = file.Close()
				}
			}()

			//把redis-trib命令的字符串构建出来
			redisTribCommand := redisTribCreateScript(clusterSizeInt, redisClusterName, ns)

			//用构建出来的正确执行命令去替换掉expectScriptTemplate模板中的exec_command_template
			execScript := strings.ReplaceAll(expectScriptTemplate, "exec_command_template", redisTribCommand)

			//写expect文件
			bufReader := bufio.NewWriter(file)
			_, _ = bufReader.Write([]byte(fmt.Sprintf(execScript)))
			//_, _ = bufReader.WriteString("string content\n")

			//保存文件,这会生成/tmp/redis-trib.sh文件
			_ = bufReader.Flush()

			//给/tmp/redis-trib.sh赋予可执行权限，以在pod的command中执行它
			_ = exec.Command("/bin/bash", "-c", "chmod +x "+expectScriptFile).Run()
		}
		
	} else if opType == "scale" {
		//扩展或者缩容集群
		//获取系统环境变量，需要获取4个：
		// 扩容前集群的规模大小,扩容后集群的大小,redisClusterName实例的名字,namespace
		oldClusterSize := os.Getenv("OLD_CLUSTER_SIZE")
		oldClusterSizeInt,_ := strconv.Atoi(oldClusterSize)
		newClusterSize := os.Getenv("NEW_CLUSTER_SIZE")
		newClusterSizeInt,_ := strconv.Atoi(newClusterSize)
		redisClusterName := os.Getenv("REDISCLUSTER_NAME")
		ns := os.Getenv("NAMESPACE")
		if len(oldClusterSize) == 0 || len(newClusterSize) == 0 ||
			len(redisClusterName) == 0 || len(ns) == 0{
			panic(errors.New("读取环境变量出错"))
		}

		//判断所有redis cluster的节点是否都已开始监听6379端口
		envReady := checkRedisClusterNodeReady(newClusterSizeInt, redisClusterName, ns)

		//如果集群节点redis服务都已经启动正常
		//准备使用reids-trib来初始化集群
		//使用redis-trib做初始化，必须要输入yes，所以使用expect来实现
		//这需要构建scale shell脚本
		if envReady {
			//检查这个文件是否存在，如果不存在就创建
			exists, err := checkFileExists(scaleScriptFile)
			if err != nil {
				log.Fatal(err)
			}
			if !exists {
				_, err := os.Create(scaleScriptFile)
				if err != nil {
					panic(err)
				}
			} else {
				err := os.Remove(scaleScriptFile)
				if err != nil {
					panic(err)
				}
			}

			//打开文件
			file, err := os.OpenFile(scaleScriptFile, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0666)
			if err != nil {
				log.Println("Fail to open file:", err)
			}

			defer func() {
				if err != nil {
					file.Close()
				} else {
					err = file.Close()
				}
			}()

			//判断是做扩容还是做缩容
			//如果是做扩容
			if newClusterSizeInt > oldClusterSizeInt {

				//把redis-trib做scale的命令字符串构建出来
				addNodeCommand,reShardInfoArray := redisTribAddScript(oldClusterSizeInt,newClusterSizeInt,
					redisClusterName, ns)

				if len(addNodeCommand) > 0 && len(reShardInfoArray) > 0 {

					for _, reShard := range reShardInfoArray {
						//语法格式：redis-trib.rb reshard --from a8b3d0f9b12d63dab3b7337d602245d96dd55844
						// --to f413fb7e6460308b17cdb71442798e1341b56cbc
						// --slots 10923 --yes --pipeline 20 127.0.0.1:6383
						reShardCommand := "redis-trib reshard "  + "--from all --to " +
							reShard.nodeIDReceiving + " --slots " +
							strconv.Itoa(reShard.slotCountByMasterMgmt) + " --yes "  + reShard.clusterInfoNode + ";\n"
						addNodeCommand += reShardCommand + "sleep 5;\n"
					}

					//用构建出来的正确执行命令去替换掉expectScriptTemplate模板中的exec_command_template
					execScript := strings.ReplaceAll(addScriptTemplate, "exec_command_template", addNodeCommand)

					//写shell文件
					bufReader := bufio.NewWriter(file)
					_, _ = bufReader.Write([]byte(fmt.Sprintf(execScript)))
					//_, _ = bufReader.WriteString("string content\n")

					//保存文件,这会生成/tmp/redis-trib-scale.sh文件
					_ = bufReader.Flush()

					//给/tmp/redis-trib.sh赋予可执行权限，以在pod的command中执行它
					_ = exec.Command("/bin/bash", "-c", "chmod +x "+scaleScriptFile).Run()
				} else {
					log.Printf("无法解析出add-node命令")
				}

			} else { //做缩容
				//TODO 待实现缩容逻辑
			}

		}
		
	}
	
}

//fullname: rediscluster01-0.rediscluster01.default.svc.cluster.local
//扩容构造一个类似于这样的脚本：
//redis-trib add-node 172.16.73.157:6379 172.16.73.166:6379，
// 这个 172.16.73.166是任意一个现有集群中的节点，使用rediscluster01-0的ip
//redis-trib add-node 172.16.73.158:6379 172.16.73.166:6379
func redisTribAddScript(oldClusterSizeInt,newClusterSizeInt int,
	redisClusterName string,ns string) (string,[]reShardInfo) {
	var resultSlice []string
	var reShardInfoArray []reShardInfo

	//构建rediscluster01-0的ip
	//得到的结果：172.16.73.166:6379
	rediscluster01String := redisClusterName + "-0" + "." +
		redisClusterName + "." + ns + ".svc.cluster.local"

	rediscluster01IP, _ := fetchIPByFullName(rediscluster01String)
	rediscluster01IPPort := rediscluster01IP + ":6379"

	masterCount := oldClusterSizeInt/2

	for i:=oldClusterSizeInt; i< newClusterSizeInt;i++ {
		itemFullName := redisClusterName + "-" + strconv.Itoa(i) + "." +
			redisClusterName + "." + ns + ".svc.cluster.local"

		itemIP, _ := fetchIPByFullName(itemFullName)

		//给这个ip加上:6379，加入slice当中
		item := fmt.Sprintf("%v:6379 ",itemIP)
		scripItem := item + " " + rediscluster01IPPort
		resultSlice = append(resultSlice,scripItem)

		//在以副本数为1的假定条件下，每2个节点，一个master，一个slave
		//每增加一个master，做一次reshard
		if i % 2 == 0 {
			reShardInfo := reShardInfo{}
			reShardInfo.clusterInfoNode = rediscluster01IPPort
			reShardInfo.nodeIDReceiving = fetchIDByIP(itemIP)
			reShardInfo.slotCountByMasterMgmt = 4096/(masterCount + 1)
			reShardInfoArray = append(reShardInfoArray,reShardInfo)
		}
		masterCount += 1

	}

	//轮询这个slice，把所有元素拼接成：
	//多行：redis-trib add-node 172.16.73.157:6379 172.16.73.166:6379，并返回
	result := ""
	for k,stringItem := range resultSlice {
		if k % 2 == 0 {
			result +=  "redis-trib add-node " + stringItem + ";" + "\n"
			result += "sleep 5; \n"
		} else {
			result +=  "redis-trib add-node --slave " + stringItem + ";" + "\n"
			result += "sleep 5; \n"
		}

	}
	return result,reShardInfoArray
}

//fullname: rediscluster01-0.rediscluster01.default.svc.cluster.local
//构造一个类似于这样的脚本：
// redis-trib create --replicas 1 172.16.73.157:6379 172.16.73.166:6379 172.16.73.169:6379 172.16.73.157:6379 172.16.73.166:6379 172.16.73.169:6379
func redisTribCreateScript(clusterSize int,redisClusterName string,ns string) string {
	var resultSlice []string
	resultSlice = append(resultSlice,"redis-trib ","create ","--replicas 1 ")

	for i:=0; i< clusterSize;i++ {
		itemFullname := redisClusterName + "-" + strconv.Itoa(i) + "." +
			redisClusterName + "." + ns + ".svc.cluster.local"

		//根据fullname去获取ip，得到ip这个string结尾有换行符，把它trim掉
		itemIP, _ := fetchIPByFullName(itemFullname)
		formatItemIP := strings.Trim(itemIP,"\n")

		//给这个ip加上:6379，加入slice当中
		item := fmt.Sprintf("%v:6379 ",formatItemIP)
		resultSlice = append(resultSlice,item)
	}

	//轮询这个slice，把所有元素拼接成整个字符串，并返回
	result := ""
	for _,stringItem := range resultSlice {
		result +=  stringItem
	}
	return result
}
