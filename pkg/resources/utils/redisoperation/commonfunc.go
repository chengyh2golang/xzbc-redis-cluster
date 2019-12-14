package main

import (
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

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
