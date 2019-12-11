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

//定义expect的模板文件
const expectScriptTemplate=`#!/bin/bash
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
auto_create_redis_cluster`

func main() {
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
		expectScriptFile := "/tmp/redis-trib.sh"

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
		defer file.Close()

		//把redis-trib命令的字符串构建出来
		redisTribCommand := redisTribScript(clusterSizeInt, redisClusterName, ns)

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
}

//fullname: rediscluster01-0.rediscluster01.default.svc.cluster.local
//构造一个类似于这样的脚本：
// redis-trib create --replicas 1 172.16.73.157:6379 172.16.73.166:6379 172.16.73.169:6379 172.16.73.157:6379 172.16.73.166:6379 172.16.73.169:6379
func redisTribScript(clusterSize int,redisClusterName string,ns string) string {
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

func fetchIPByFullName (fullName string) (string,error) {
	//通过dig命令去解析fullname对应的ip地址
	cmd := exec.Command("/bin/bash", "-c", "dig +short "+fullName)
	//得到解析的ip
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("fetch ip by fullname failed")
		return "", nil
	}
	return string(output), nil
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
