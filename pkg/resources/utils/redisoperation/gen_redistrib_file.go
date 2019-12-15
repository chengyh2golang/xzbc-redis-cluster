package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)


const (
	scaleScriptFile = "/tmp/redis-trib-scale.sh"
	expectScriptFile = "/tmp/redis-trib-create.sh"
	statusTempFile = "/tmp/status.txt"

	scaleScriptTemplate=`#!/bin/bash

scale_redis_cluster() {
exec_command_template
}

## call func
scale_redis_cluster
`

	//定义创建集群需要用的expect模板文件
	expectScriptTemplate=`#!/bin/bash
#!/usr/bin/expect

auto_create_redis_cluster() {
    expect -c "set timeout -1;
        spawn exec_command_template; 
        expect {
            *accept): {send -- yes\r;exp_continue;}
            eof {exit 0;}
        }";
}

## call func
auto_create_redis_cluster
`

	// 需要生成类似这样一个redis-trib的扩容脚本
	/*
	   #!/bin/bash
	   add_redis_cluster() {
	   redis-trib add-node 172.16.0.31:6379  172.16.0.24:6379;
	   sleep 5;
	   redis-trib add-node --slave 172.16.0.32:6379  172.16.0.24:6379;
	   sleep 5;
	   redis-trib reshard --from all --to 31810e94154682e5aaf831a48a836ee9f70a222d --slots 4096 --yes 172.16.0.24:6379;
	   }
	   ## call func
	   add_redis_cluster
	*/
	addScriptTemplate=`#!/bin/bash

add_redis_cluster() {
exec_command_template
}

## call func
add_redis_cluster
`

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

type slaveInfo struct {
	slaveIP string
	slaveID string
}

type redisClusterMasterInfo struct {
	masterIP string
	masterID string
	slaveCount int
	slaveInfoArr []slaveInfo
	slot int
}

type redisClusterSlaveInfo struct {
	slaveInfo
	masterIP string
	masterID string
}

func main() {
	//首先获取CLUSTER_OP_TYPE这个系统环境变量
	//如果是"create"，就走创建集群的逻辑，如果是"scale"，就走扩容或者缩容逻辑

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

		checkSize := 0
		//判断节点都在运行，不管是扩容还是缩容，都需要所有节点都在运行
		if newClusterSizeInt > oldClusterSizeInt {
			checkSize = newClusterSizeInt
		}  else {
			checkSize = oldClusterSizeInt
		}

		//判断所有redis cluster的节点是否都已开始监听6379端口
		envReady := checkRedisClusterNodeReady(checkSize, redisClusterName, ns)

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
				//以6节点的集群为例，判断pod-5是不是主节点，如果是主节点，将它的从节点移除
				//redis-trib check
				//获取当前的集群状态,通过连接集群第一个节点去获取rediscluster01-0.rediscluster01

				//获取rediscluster01-0.rediscluster01的ip地址
				rediscluster01String := redisClusterName + "-0" + "." +
					redisClusterName + "." + ns + ".svc.cluster.local"

				//根据rediscluster01-0的ip去获取集群的当前状态
				rediscluster01IP, _  := fetchIPByFullName(rediscluster01String)
				rediscluster01IPPort := rediscluster01IP + ":6379"

				clusterStatusStr := fetchClusterStatus(rediscluster01IP)

				//将返回结果clusterStatusStr写入/tmp/status.txt文件
				// 这个文件名是由常量statusTempFile定义的
				err := writeStatusFile(statusTempFile, clusterStatusStr)
				if err != nil {
					log.Printf("写入临时状态文件出错：%v",err)
					panic(err)
				}

				//从临时状态文件中获取集群状态信息
				masterInfoMap, slaveInfoMap := fetchRedisClusterInfo(statusTempFile)
				log.Println(masterInfoMap,"\n",slaveInfoMap)

				//判断要被缩容的节点信息，例如当前是6个节点，需要被缩容成4个节点
				//判断rediscluster01-5和rediscluster01-5是slave还是master
				//如果是slave，就直接移除
				//如果是master，先移除master的slave，再迁移走master上的slot，再移除master

				execCommandTemplate := ""

				//定义已经被移除的slave节点
				var removedSlaveID []string

				for i:= oldClusterSizeInt-1; i< newClusterSizeInt;i-- {
					//根据要移除的主机名去获取对应的ip
					itemFullName := redisClusterName + "-" + strconv.Itoa(i) + "." +
						redisClusterName + "." + ns + ".svc.cluster.local"

					itemIP, _ := fetchIPByFullName(itemFullName)

					//根据itemIP获取节点在集群中的状态信息
					if masterStruct, ok  := masterInfoMap[itemIP]; ok {
						//这是master节点，先将master节点对应的slave节点移除
						for _,slave := range masterStruct.slaveInfoArr {
							//移除slave节点
							execLine := "redis-trib.rb del-node " + rediscluster01IPPort + " " +
								slave.slaveID + ";\n"

							execLine += "sleep 5; \n"
							removedSlaveID = append(removedSlaveID,slave.slaveID)
							execCommandTemplate += execLine
							fmt.Println(execCommandTemplate)
						}

						//移除完所有的slave之后，重新分配该master节点上的slot
						//判断集群中的master数量，来确定做几次shard
						newClusterMasterCount := newClusterSizeInt / 2

						//定义一个已经移动了的shard数量
						reshardedCount := 0

						//开始做reshard 这个master节点上slot的逻辑
						for j:= 0;j<newClusterMasterCount;j++ {
							count := 0

							//做这个判断的逻辑在于，要把slot全部移除完，如果用平均数，可能无法把slot全部移除
							//如果是最后一次循环，就把剩余的slot全部移除
							if j +1 == newClusterMasterCount {
								count = masterStruct.slot - reshardedCount
							} else {
								count = masterStruct.slot / newClusterMasterCount
							}

							reshardToMasterName := redisClusterName + "-" + strconv.Itoa(j) + "." +
								redisClusterName + "." + ns + ".svc.cluster.local"
							reshardToMasterIP,_ := fetchIPByFullName(reshardToMasterName)
							reshardToMasterID := masterInfoMap[reshardToMasterIP].masterID

							reShardCommand := "redis-trib reshard "  + "--from" + masterStruct.masterID +
								" --to " +
								reshardToMasterID + " --slots " + strconv.Itoa(count) +
								  " --yes "  + rediscluster01IPPort + ";\n"
							reShardCommand += "sleep 5;\n"
							execCommandTemplate += reShardCommand

							//把本次移动的数量加给reshardedCount
							reshardedCount += count
						}

						//移除完master上的slot之后，移除这个master
						execMasterLine := "redis-trib.rb del-node " + rediscluster01IPPort + " " +
							masterStruct.masterID + ";\n"
						execMasterLine += "sleep 5; \n"

						execCommandTemplate += execMasterLine

					} else {
						//这是slave节点
						//判断这个slave节点是否已经被移除，如果没有，移除它
						slave := slaveInfoMap[itemIP]
						if !isElementExistsInArr(slave.slaveID,removedSlaveID) {
							execLine := "redis-trib.rb del-node " + rediscluster01IPPort + " " +
								slave.slaveID + ";\n"

							execLine += "sleep 5; \n"
							removedSlaveID = append(removedSlaveID,slave.slaveID)
							execCommandTemplate += execLine
						}
					}
				}

				//exec_command_template将写入shell脚本
				//用构建出来的正确执行命令去替换掉expectScriptTemplate模板中的exec_command_template
				execScript := strings.ReplaceAll(scaleScriptTemplate, "exec_command_template", execCommandTemplate)

				//写shell文件
				bufReader := bufio.NewWriter(file)
				_, _ = bufReader.Write([]byte(fmt.Sprintf(execScript)))
				//_, _ = bufReader.WriteString("string content\n")

				//保存文件,这会生成/tmp/redis-trib-scale.sh文件
				_ = bufReader.Flush()

				//给/tmp/redis-trib.sh赋予可执行权限，以在pod的command中执行它
				_ = exec.Command("/bin/bash", "-c", "chmod +x "+scaleScriptFile).Run()

			}

		}
		
	}
	
}


//检查一个元素是否存在于一个数组中
func isElementExistsInArr(str string, arr []string) bool {
	for _,value := range arr {
		if value == str {
			return true
		}
	}
	return false
}

//将redis-trib check获取的string状态信息，写入到临时文件中
func writeStatusFile(filename string,content string) error {
	_, err := os.Create(filename)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0666)
	if err != nil {
		log.Println("Fail to open file:", err)
		return err
	}

	defer func() {
		if err != nil {
			file.Close()
		} else {
			err = file.Close()
		}
	}()

	bufReader := bufio.NewWriter(file)
	_, _ = bufReader.Write([]byte(content))

	//保存文件
	_ = bufReader.Flush()
	return nil
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

//根据redis-trib连接集群中的任意一个节点获取整个集群的状态信息
//默认就选择rediscluster01-0.rediscluster01即集群的第一个ip
func fetchClusterStatus(ip string) string {
	cmd := exec.Command("/bin/bash",
		"-c",
		"redis-trib check " + ip + ":6379",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("fetch info by ip failed")
		return ""
	}
	return string(output)
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

//下面是做缩容时用到的公共方法
func fetchRedisClusterInfo(filename string)(
	map[string]redisClusterMasterInfo,
	map[string]redisClusterSlaveInfo)  {

	//[]map "master0" []line1_string,line2_string,line3_string
	//一个for循环把文件内容跟master或者slave有关的3行记录分别读进2个数组中
	//定义master和slave数组
	masterArrMap := map[string][]string{}
	slaveArrMap := map[string][]string{}

	//打开文件
	fd, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return nil,nil
	}
	defer fd.Close()

	//读取文件
	br := bufio.NewReader(fd)

	//将跟每一个master和slave的信息读取到masterStrArr和slaveStrArr这2个数组中
	var masterStrArr []string
	var slaveStrArr []string

	//通过一个开关来控制是一个master或者slave信息的开始
	//如果一个行里包括M:，说明这是master信息的开始行，把这一行及后面2行的信息存入到这个数组中
	var isMasterContent  bool
	var isSlaveContent bool

	//map格式是：map[string][]string，map的key的名字是master0，master1，master2
	//或者slave0,slave1,slave2这样的命名，跟master和slave的数量有关。
	// 用masterCount/slaveCount来计数
	masterCount := 0
	slaveCount := 0

	//用line开控制一个master或者slave行的开始，往后读2行，一共读3行。
	line := 0

	//开始循环读取每行文件内容
	for {

		lineContent, _, c := br.ReadLine()
		lineContentStr := string(lineContent)
		if c == io.EOF {
			break
		}
		if i := strings.Index(lineContentStr, ">>>"); i != -1 {
			continue
		}

		if isMasterInfoStart(lineContentStr) {

			//设置isMasterContent为true，以读取后面的2行
			isMasterContent = true
			isSlaveContent = false

			line = 0
			//清空master中的数据，开始记录数据
			masterStrArr = []string{}

			//将当前读到的内容存入masterStrArr
			masterStrArr = append(masterStrArr,lineContentStr)

			//记录读取的行数
			line += 1
		} else if isSlaveInfoStart(lineContentStr) {
			line = 0

			isSlaveContent = true
			isMasterContent = false

			//清空slave数组中数据，开始记录数据
			slaveStrArr = []string{}

			//将当前读到的内容存入masterStrArr
			slaveStrArr = append(slaveStrArr,lineContentStr)

			//记录读取的行数
			line += 1

		} else if isMasterContent && line <= 2 {
			//将当前读到的内容存入masterStrArr
			masterStrArr = append(masterStrArr,lineContentStr)
			line += 1

			//if line==3,表示这3行master相关元素已经全部读取完成
			if line == 3 {
				masterArrMap["master" + strconv.Itoa(masterCount)] = masterStrArr
				masterCount += 1
			} else {
				continue
			}

		} else if isSlaveContent && line <= 2 {
			//将当前读到的内容存入slaveStrArr
			slaveStrArr = append(slaveStrArr,lineContentStr)
			line += 1

			//if line==3,表示这3行slave相关元素已经全部读取完成
			if line == 3 {
				slaveArrMap["slave" + strconv.Itoa(slaveCount)] = slaveStrArr
				slaveCount +=1
			} else {
				continue
			}
		}

		if strings.Index(lineContentStr,"[ok]") != -1 {
			break
		}

	}

	//定义master struct的map数组
	//var redisClusterMapArr []map[string]redisClusterMasterInfo


	//得到masterArrMap和slaveArrMap这2个数组之后，开始构建redisClusterMasterInfo数据结构

	var redisClusterMasterMap = map[string]redisClusterMasterInfo{}
	var redisClusterSlaveMap = map[string]redisClusterSlaveInfo{}

	for _,lineContentArr := range masterArrMap {
		//定义master map
		redisClusterMasterStruct := redisClusterMasterInfo{}

		//[M: 4755e7640c7c54df1653911abd515001b85817bf 172.16.73.146:6379
		// slots:0-5460 (5461 slots) master
		// 1 additional replica(s)]
		masterInfoStr := lineContentArr[0]
		redisClusterMasterStruct.masterID = strings.Split(masterInfoStr," ")[1]
		redisClusterMasterStruct.masterIP = strings.Split(strings.Split(masterInfoStr," ")[2],":")[0]
		redisClusterMasterStruct.slot = fetchMasterSlot(lineContentArr[1])
		redisClusterMasterStruct.slaveCount = fetchSlaveCount(lineContentArr[2])
		redisClusterMasterMap[redisClusterMasterStruct.masterIP] = redisClusterMasterStruct
	}

	for _,lineContentArr := range slaveArrMap {
		//定义master map
		redisClusterSlaveStruct := redisClusterSlaveInfo{}

		//[M: 4755e7640c7c54df1653911abd515001b85817bf 172.16.73.146:6379
		// slots:0-5460 (5461 slots) master
		// 1 additional replica(s)]

		//line0:M: 4755e7640c7c54df1653911abd515001b85817bf 172.16.73.146:6379
		slaveInfoStr := lineContentArr[0]
		//获取slaveID
		redisClusterSlaveStruct.slaveID = strings.Split(slaveInfoStr," ")[1]

		//获取slaveIP
		redisClusterSlaveStruct.slaveIP = strings.Split(strings.Split(slaveInfoStr," ")[2],":")[0]
		//获取slave的master ID
		redisClusterSlaveStruct.masterID = fetchSlaveMasterID(lineContentArr[2])

		//将slave信息写入redisClusterSlaveMap
		redisClusterSlaveMap[redisClusterSlaveStruct.slaveIP] = redisClusterSlaveStruct

		//轮询masterMap，将masterMap里需要的slave信息填充进去
		for k,v := range redisClusterMasterMap {
			//判断masterMap中masterIP对应的struct的ID和slave对应的MasterID是否相等
			if v.masterID == redisClusterSlaveStruct.masterID {

				//把slave的ID和IP信息的struct添加到masterMAP中的slaveInfoArr这个数组中
				v.slaveInfoArr = append(v.slaveInfoArr,slaveInfo{
					slaveIP: redisClusterSlaveStruct.slaveIP,
					slaveID: redisClusterSlaveStruct.slaveID,
				})
				//写回redisClusterMasterMap
				redisClusterMasterMap[k] = v

				//并将k对应的master的IP写入redisClusterSlaveStruct对应MasterIP字段
				redisClusterSlaveStruct.masterIP = k
			}
		}
	}
	//将填充完信息的redisClusterMasterMap和redisClusterSlaveMap返回
	return redisClusterMasterMap,redisClusterSlaveMap
}

//获取slave对应的master的id
func fetchSlaveMasterID(str string) string {
	resultArr := strings.Split(strings.TrimSpace(str), " ")
	return resultArr[len(resultArr) -1 ]
}

//获取master的slave数量
func fetchSlaveCount(str string) int {
	//数据不规范，先用strings.TrimSpace清洗掉数据的外面的空格
	result, _ := strconv.Atoi(strings.Split(strings.TrimSpace(str), " ")[0])
	return result
}

//获取master的slot数量
// slots:0-5460 (5461 slots) master
func fetchMasterSlot(str string) int {
	//"   slots:0-5460 (5461 slots) master"
	//先用strings.TrimSpace清洗掉数据的外面的空格
	result, _ := strconv.Atoi(strings.Trim(strings.Split(strings.TrimSpace(str), " ")[1], "("))
	return result
}

//判断是否是Master节点的开始信息
func isMasterInfoStart(lineContentStr string) bool {
	i := strings.Index(lineContentStr, "M:")
	if i != -1 {
		return true
	} else {
		return false
	}
}

//判断是否是Slave节点的开始
func isSlaveInfoStart(lineContentStr string) bool {
	i := strings.Index(lineContentStr, "S:")
	if i != -1 {
		return true
	} else {
		return false
	}
}