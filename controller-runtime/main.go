package main

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
//读取kubeconfig文件内容信息,读取k8s api信息
	c, _ := client.New(config.GetConfigOrDie(), client.Options{})
//使用node对象
	nodes := &corev1.NodeList{}
//获取node节点所属标签
	filter := client.MatchingLabels{
		"node-role.kubernetes.io/control-plane": "",
	}
//list标签内的value
	_ = c.List(context.TODO(), nodes, filter)
//循环切片取值
	for _, node := range nodes.Items {
		fmt.Println(node.ObjectMeta.GetName())
	}
}
