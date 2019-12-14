package main

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
