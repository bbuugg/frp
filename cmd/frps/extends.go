package main

import (
	"encoding/json"
	"fmt"
	"frp-panel/rpc"
	"os"
	"time"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/util/log"
	"github.com/google/uuid"
)

func runRegister(registryAddr string, svrCfg *v1.ServerConfig) {
	cfgJson, err := json.Marshal(svrCfg)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// 配置节点信息
	nodeInfo := &rpc.NodeInfo{
		NodeId:    uuid.NewString(),
		ServerCfg: string(cfgJson),
	}

	// 创建gRPC客户端
	client := rpc.NewNodeClient(registryAddr, nodeInfo)
	defer client.Stop()
	// 可选：配置客户端参数
	client.SetPingInterval(30 * time.Second)  // 每30秒发送一次ping
	client.SetReconnectDelay(5 * time.Second) // 重连间隔5秒
	client.SetMaxReconnectAttempts(0)         // 无限重连

	// 启动客户端
	if err = client.Start(); err != nil {
		log.Errorf("Failed to start client: %v", err)
		os.Exit(1)
	}

	//var sig = make(chan os.Signal, 1)
	//signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// 启动一个goroutine来定期打印状态
	//go func() {
	//	ticker := time.NewTicker(10 * time.Second)
	//	defer ticker.Stop()
	//
	//	for {
	//		select {
	//		case <-ticker.C:
	//			connected := client.IsConnected()
	//			registered := client.IsRegistered()
	//			nodeID := client.GetNodeID()
	//
	//			status := "disconnected"
	//			if connected {
	//				if registered {
	//					status = fmt.Sprintf("connected and registered (ID: %s)", nodeID)
	//				} else {
	//					status = "connected but not registered"
	//				}
	//			}
	//
	//			fmt.Printf("Status: %s", status)
	//		case <-sig:
	//			return
	//		}
	//	}
	//}()
	//
	//<-sig
	//// 优雅停止客户端
	//if err = client.Stop(); err != nil {
	//	fmt.Printf("Error stopping client: %v", err)
	//	return err
	//}
}
