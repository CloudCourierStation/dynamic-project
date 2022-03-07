package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// VMGVR 定义资源的GVR，以供dynamic client识别资源
var VMGVR = schema.GroupVersionResource{
	Group:    "cloud.waibizi.com",
	Version:  "v1",
	Resource: "virtualmachines",
}

//VirtualMachine 根据 CRD 定义 的 结构体
type VirtualMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VMSpec `json:"spec"`
}

type VMSpec struct {
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Memory int    `json:"memory"`
	Disk   int    `json:"disk"`
	Status string `json:"status"`
}
