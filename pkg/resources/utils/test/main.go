package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

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
	//bsPath, _ := filepath.Abs("./check.txt")
	//bytes, err := ioutil.ReadFile(bsPath)
	pwd, _ := os.Getwd()
	filename := pwd + "/pkg/resources/utils/test/check.txt"
	//fmt.Println(filename)

	//bytes, _ := ioutil.ReadFile(checkPwd +"/check.txt")
	redisClusterMasterInfo, redisClusterSlaveInfo := fetchRedisClusterInfo(filename)
	fmt.Println(redisClusterMasterInfo,redisClusterSlaveInfo)
}

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
