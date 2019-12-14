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
const addScriptTemplate=`#!/bin/bash

add_redis_cluster() {
exec_command_template
}

## call func
add_redis_cluster
reshard_redis_cluster
`

func main() {
	//获取系统环境变量，需要获取3个：
	// 集群的规模大小
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
			redisTribCommand := redisTribAddScript(oldClusterSizeInt,newClusterSizeInt,
				redisClusterName, ns)

			//用构建出来的正确执行命令去替换掉expectScriptTemplate模板中的exec_command_template
			execScript := strings.ReplaceAll(addScriptTemplate, "exec_command_template", redisTribCommand)

			//写shell文件
			bufReader := bufio.NewWriter(file)
			_, _ = bufReader.Write([]byte(fmt.Sprintf(execScript)))
			//_, _ = bufReader.WriteString("string content\n")

			//保存文件,这会生成/tmp/redis-trib-scale.sh文件
			_ = bufReader.Flush()

			//给/tmp/redis-trib.sh赋予可执行权限，以在pod的command中执行它
			_ = exec.Command("/bin/bash", "-c", "chmod +x "+scaleScriptFile).Run()
		} else { //做缩容

		}

	}
}

//fullname: rediscluster01-0.rediscluster01.default.svc.cluster.local
//扩容构造一个类似于这样的脚本：
//redis-trib add-node 172.16.73.157:6379 172.16.73.166:6379，
// 这个 172.16.73.166是任意一个现有集群中的节点，使用rediscluster01-0的ip
//redis-trib add-node 172.16.73.158:6379 172.16.73.166:6379
func redisTribAddScript(oldClusterSizeInt,newClusterSizeInt int,redisClusterName string,ns string) string {
	var resultSlice []string

	//构建rediscluster01-0的ip
	//得到的结果：172.16.73.166:6379
	rediscluster01String := redisClusterName + "-0" + "." +
		redisClusterName + "." + ns + ".svc.cluster.local"

	rediscluster01IP, _ := fetchIPByFullName(rediscluster01String)
	rediscluster01IPPort := rediscluster01IP + ":6379"

	for i:=oldClusterSizeInt; i< newClusterSizeInt;i++ {
		itemFullName := redisClusterName + "-" + strconv.Itoa(i) + "." +
			redisClusterName + "." + ns + ".svc.cluster.local"

		itemIP, _ := fetchIPByFullName(itemFullName)

		//给这个ip加上:6379，加入slice当中
		item := fmt.Sprintf("%v:6379 ",itemIP)
		scripItem := item + " " + rediscluster01IPPort
		resultSlice = append(resultSlice,scripItem)
	}

	//轮询这个slice，把所有元素拼接成：
	//多行：redis-trib add-node 172.16.73.157:6379 172.16.73.166:6379，并返回
	result := ""
	for k,stringItem := range resultSlice {
		if k % 2 == 0 {
			result +=  "redis-trib add-node " + stringItem + ";" + "\n"
		} else {
			result +=  "redis-trib add-node --slave " + stringItem + ";" + "\n"
		}

	}

	//构建redis-trib reshard

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
