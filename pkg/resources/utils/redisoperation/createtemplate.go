package main

//定义创建集群需要用的expect模板文件
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
auto_create_redis_cluster
`


