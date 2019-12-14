package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const scaleScriptFile = "/tmp/redis-trib-scale.sh"

//定义redis-trib做扩容的模板文件
// 需要生成类似这样一个redis-trib的扩容脚本
/*
#!/bin/bash

add_redis_cluster() {
redis-trib add-node 172.16.0.31:6379  172.16.0.24:6379;
sleep 5;
redis-trib add-node --slave 172.16.0.32:6379  172.16.0.24:6379;
sleep 5;
redis-trib add-node 172.16.0.33:6379  172.16.0.24:6379;
sleep 5;
redis-trib add-node --slave 172.16.0.34:6379  172.16.0.24:6379;
sleep 5;
redis-trib reshard --from all --to 31810e94154682e5aaf831a48a836ee9f70a222d --slots 4096 --yes 172.16.0.24:6379;
redis-trib reshard --from all --to ae8ae95eb83d31015f4292e48f7faea3d7d46d8d --slots 2730 --yes 172.16.0.24:6379;

}

## call func
add_redis_cluster
 */
const addScriptTemplate=`#!/bin/bash

add_redis_cluster() {
exec_command_template
}

## call func
add_redis_cluster
`

type reShardInfo struct {
	//执行reShard时，需要指定一个现有集群中的节点，格式：ip:6379
	//使用的是集群的第一个节点，它的域名类似：rediscluster01-0.rediscluster01.default.svc.cluster.local
	//基于这个域名去获取ip
	clusterInfoNode string
	slotCountByMasterMgmt int //为了平衡每个master管理的slot的个数,16384/master个数
	nodeIDReceiving string //接收slot的node id，指定的是新加进来的master，新增4个节点，就是2个master
	//sourceNodeID string  //使用all
}

func main() {
	//获取系统环境变量，需要获取4个：
	// 扩容前集群的规模大小和扩容后集群的大小
	// redisClusterName实例的名字
	// namespace
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
					addNodeCommand += reShardCommand
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
			reShardInfo.slotCountByMasterMgmt = 16384/(masterCount + 1)
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

//检查文件是否存在
func checkFileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}


//redis-trib check 172.16.0.31:6379 | grep 172.16.0.31 | grep -v Check | awk '{print $2}'
//得到70451029303870d124cc74cb8e4fae9962f748b8这样一个id
func fetchIDByIP(ip string) string {
	cmd := exec.Command("/bin/bash",
		"-c",
		"redis-trib check " + ip + ":6379" +
		" | grep " + ip + " | grep -v Check | awk '{print $2}'",
		)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("fetch id by ip failed")
		return ""
	}
	return strings.Trim(string(output),"\n")
}

func fetchIPByFullName (fullName string) (string,error) {
	//通过dig命令去解析fullname对应的ip地址
	cmd := exec.Command("/bin/bash", "-c", "dig +short "+fullName)
	//得到解析的ip
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("fetch ip by fullname failed")
		return "", nil
	}
	return strings.Trim(string(output),"\n"), nil
}

//检查环境是否就绪
//检查所有目标redis节点的6379端口都已开始监听
func checkRedisClusterNodeReady(clusterSizeInt int,redisClusterName,ns string) bool {
	var readyNode = map[string]string{}
	for ! (len(readyNode) == clusterSizeInt)  {
		time.Sleep(time.Second *1)
		for i:=0;i<clusterSizeInt;i ++ {
			//构造一个fullname
			itemFullname := redisClusterName + "-" + strconv.Itoa(i) + "." +
				redisClusterName + "." + ns + ".svc.cluster.local"

			//基于fullname通过diag去获取对应的ip，返回值结尾带有字符串，需要trim掉
			itemIP, _ := fetchIPByFullName(itemFullname)
			formatItemIPAndPort := strings.Trim(itemIP,"\n")+":6379"

			//依据节点的ip做tcp的端口检查，是否监听
			ready := checkRedisNodeReady(formatItemIPAndPort)

			//如果该节点的6369处于监听状态且readyNode这个map没有这条数据，就写入map
			if ready {
				if readyNode[formatItemIPAndPort]  == "" {
				readyNode[formatItemIPAndPort] = formatItemIPAndPort
				}  else{ //如果已经监听，且map中已经有值，跳出本次循环
					continue
				}
			} else { //如果有节点端口还没有监听，结束内层循环，到外层循环，先睡1秒后，继续检查
				break
			}
			log.Printf("已经就绪的节点个数：%v, 就绪的节点: %v",len(readyNode),readyNode)
		}
	}
	return true
}

//检查端口是否监听
func checkRedisNodeReady(ip string) bool {
	_, err := net.Dial("tcp", ip)
	if err != nil {
		return false
	}
	return true
}
