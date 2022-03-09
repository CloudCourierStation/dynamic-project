# 实战dynamic-client与深入理解

## 1.前言

最近写operator的时候，CRD不是我自个定义的，没有生成自定义的clientSet，于是使用了dynamic-client，顺带做做笔记分享出来。

课程工程源码放到GitHub了

`https://github.com/CloudCourierStation/dynamic-project`

## 2.CRD定义

CRD还是使用以前文章当中使用的一个定义，

```yaml
# virtualmachines-crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  # name 必须匹配下面的spec字段：<plural>.<group>  
  name: virtualmachines.cloud.waibizi.com
spec:
   # name 必须匹配下面的spec字段：<plural>.<group>  
  group: cloud.waibizi.com
  # group 名用于 REST API 中的定义：/apis/<group>/<version>  
  versions:
  - name: v1 # 版本名称，比如 v1、v2beta1 等等    
    served: true  # 版本名称，比如 v1、v2beta1 等等    
    storage: true  # 是否开启通过 REST APIs 访问 `/apis/<group>/<version>/...`    
    schema:  # 定义自定义对象的声明规范 
      openAPIV3Schema:
        description: Define virtualMachine YAML Spec
        type: object
        properties:
          # 自定义CRD的字段类型
          spec:
            type: object
            properties:
              uuid:
                type: string
              name:
                type: string
              image:
                type: string
              memory:
                type: integer
              disk:
                type: integer
              status:
                type: string
  # 定义作用范围：Namespaced（命名空间级别）或者 Cluster（整个集群）              
  scope: Namespaced
  names:
    # 定义作用范围：Namespaced（命名空间级别）或者 Cluster（整个集群）
    kind: VirtualMachine
     # plural 名字用于 REST API 中的定义：/apis/<group>/<version>/<plural>    
    plural: virtualmachines
     # singular 名称用于 CLI 操作或显示的一个别名 
    singular: virtualmachines
# 这个地方就是平时使用kubectl get po 当中这个 po 是 pod的缩写的定义，我们可以直接使用kubectl get vm查看
    shortNames:
    - vm
```

顺便增加一条CR进去

```yaml
# public-wx.yaml
apiVersion: "cloud.waibizi.com/v1"
kind: VirtualMachine
metadata:
  name: public-wx
spec:
  uuid: "2c4789b2-30f2-4d31-ab71-ca115ea8c199"
  name: "waibizi-wx-virtual-machine"
  image: "Centos-7.9"
  memory: 4096
  disk: 500
  status: "creating"
```

## 3.dynamic-client实战

```shell
mkdir dynamic-project && cd dynamic-project
go mod init dynamic-project
go get k8s.io/client-go@v0.22.1
go get k8s.io/apimachinery@v0.22.1
```

稍微新建几个文件夹跟文件

```shell
tree
.
├── go.mod
├── go.sum
├── main.go
└── pkg
    └── apis
        ├── v1
        │   └── type.go
        └── vm_controller.go

3 directories, 5 files
```

### main.go

```go
package main

import (
	"dynamic-project/pkg/apis"
	"flag"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog"
	"path/filepath"
)

func main() {
	var err error
	var config *rest.Config
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(可选) kubeconfig 文件的绝对路径")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "kubeconfig 文件的绝对路径")
	}
	flag.Parse()
	if config, err = rest.InClusterConfig(); err != nil {
		// 使用 KubeConfig 文件创建集群配置 Config 对象
		if config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig); err != nil {
			panic(err.Error())
		}
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return
	}
	dynamicSharedInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, 0, "default", nil)
	// 初始化controller，传入client、informer, 
	controller := apis.NewController(dynamicClient, dynamicSharedInformerFactory)
	// 直接启动controller
	err = controller.Run()
	if err != nil {
		klog.Errorln("fail run controller")
		return
	}
}
```

### type.go

```go
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

```

### vm_controller.go

```go
package apis

import (
	"context"
	v1 "dynamic-project/pkg/apis/v1"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"time"
)

var stopCh chan struct{}

const controllerAgentName = "vm-dynamic-controller"

type Controller struct {
	dynamicClient dynamic.Interface
	workqueue     workqueue.RateLimitingInterface
	vmSynced      cache.InformerSynced
	vmInformer    cache.SharedIndexInformer
	vmLister      dynamiclister.Lister
	recorder      record.EventRecorder
}

func NewController(
	dynamicClient dynamic.Interface,
	factory dynamicinformer.DynamicSharedInformerFactory) *Controller {
	informer := factory.ForResource(v1.VMGVR).Informer()
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	controller := &Controller{
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "virtualmachines"),
		vmSynced:      informer.HasSynced,
		vmInformer:    informer,
		vmLister:      dynamiclister.New(informer.GetIndexer(), v1.VMGVR),
		recorder:      recorder,
		dynamicClient: dynamicClient,
	}
	// 添加回调事件
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.enqueueFoo(obj)
		},
	})

	return controller
}

func (c *Controller) enqueueFoo(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) Run() error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()
	go c.vmInformer.Run(stopCh)
	// 启动worker,每个worker一个goroutine
	go wait.Until(c.runWorker, time.Second, stopCh)
	stopCh = make(chan struct{})
	// 等待cache同步
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.vmSynced); !ok {
		klog.Errorln("failed to wait for caches to sync")
		return nil
	}
	// 等待退出信号
	<-stopCh
	klog.Infoln("Shutting down workers")
	return nil
}

// worker就是一个循环不断调用processNextWorkItem
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	// 从工作队列获取对象
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			// 处理失败再次加入队列
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// 处理成功不入队
		c.workqueue.Forget(obj)
		klog.Infoln("Successfully synced '%s'", key)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return err
	}
	unStruct, err := c.vmLister.Namespace(namespace).Get(name)
	newBytes, err := json.Marshal(unStruct)
	if err != nil {
		utilruntime.HandleError(err)
		return err
	}
	vm := &v1.VirtualMachine{}
	if err = json.Unmarshal(newBytes, vm); err != nil {
		utilruntime.HandleError(err)
		return err
	}
	if vm.Spec.Status == "creating" {
		err = c.Creating(vm)
		if err != nil {
			utilruntime.HandleError(err)
			return err
		}
	}
	return nil
}

func (c *Controller) Creating(vm *v1.VirtualMachine) error {
	// 假设虚拟机都可以创建成功，不成功的话就直接更新为fail啥的就行了，或者返回err重新加入队列当中，不断地去进行创建操作
	patchData := []byte(`{"spec": {"status": "complete"}}`)
	// patch更新CR
	_, err := c.dynamicClient.Resource(v1.VMGVR).Namespace("default").Patch(context.Background(),vm.Name,types.MergePatchType,patchData,metav1.PatchOptions{})
	if err != nil {
		klog.Errorln(err)
		return err
	}
	return nil
}
```

## 4.`controller`的结构体分析

`controller`的结构体

```go
type Controller struct {
	dynamicClient dynamic.Interface
	workqueue     workqueue.RateLimitingInterface
	vmSynced      cache.InformerSynced
	vmInformer    cache.SharedIndexInformer
	vmLister      dynamiclister.Lister
	recorder      record.EventRecorder
}
```

### dynamicClient dynamic.Interface

```go
// 简单过一下创建逻辑
// NewForConfig 创建了一个新的元数据客户端，它可以以检索任何 Kubernetes 对象（核心、聚合或基于自定义资源）的元数据详细信息(PartialObjectMetadata 对象的形式)，或者返回错误。
func NewForConfig(inConfig *rest.Config) (Interface, error) {
  // 复制并设置默认的属性
	config := ConfigFor(inConfig)
	// 用于序列化选项
	config.GroupVersion = &schema.GroupVersion{}
	config.APIPath = "/this-value-should-never-be-sent"
  // 获取包装了http.client的client，操作obj，后面章节讲解
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}
	return &dynamicClient{client: restClient}, nil
}

// Resource 返回一个接口，该接口可以访问集群或命名空间范围的资源实例。
func (c *Client) Resource(resource schema.GroupVersionResource) Getter {
	return &client{client: c, resource: resource}
}
```

### workqueue.RateLimitingInterface

```go
// 常见的有三种队列：通用队列、限速队列、延时队列；基本都是一层套一层的
// 先看通用队列
type Interface interface {
    Add(item interface{})                   // 向队列中添加一个元素，interface{}类型，说明可以添加任何类型的元素
    Len() int                               // 队列长度，就是元素的个数
    Get() (item interface{}, shutdown bool) // 从队列中获取一个元素，双返回值
    Done(item interface{})                  // 告知队列该元素已经处理完了
    ShutDown()                              // 关闭队列
    ShuttingDown() bool                     // 查询队列是否正在关闭
}
// 延时队列
type DelayingInterface interface {
    Interface                                          // 继承了通用队列所有接口                   
    AddAfter(item interface{}, duration time.Duration) // 增加了延迟添加的接口
}

type RateLimitingInterface interface {
    DelayingInterface                 // 继承了延时队列
    AddRateLimited(item interface{})  // 按照限速方式添加元素的接口
    Forget(item interface{})          // 丢弃指定元素
    NumRequeues(item interface{}) int // 查询元素放入队列的次数
}

```

### cache.InformerSynced

InformerSynced是一个函数，用于判断一个informer是否已同步。这对于确定缓存是否已同步非常有用。

###  cache.SharedIndexInformer

```go
// 这个字段的初始化我们需要追溯到main.go调用NewFilteredDynamicSharedInformerFactory的时候
// NewFilteredDynamicSharedInformerFactory 构造了一个 dynamicSharedInformerFactory 的新实例。 
// 通过此工厂获得的lister将受到此处指定的相同过滤器的约束。
func NewFilteredDynamicSharedInformerFactory(client dynamic.Interface, defaultResync time.Duration, namespace string, tweakListOptions TweakListOptionsFunc) DynamicSharedInformerFactory {
	return &dynamicSharedInformerFactory{
		client:           client,
		defaultResync:    defaultResync,
		namespace:        namespace,
		informers:        map[schema.GroupVersionResource]informers.GenericInformer{},
		startedInformers: make(map[schema.GroupVersionResource]bool),
		tweakListOptions: tweakListOptions,
	}
}

type dynamicSharedInformerFactory struct {
  // 构建ListWatch接口使用，为后来构建reflector，执行listWatch监控api resource提供client
	client        dynamic.Interface
  // 同步周期，informer同步deltaFIFO中数据到listener中的chan中
	defaultResync time.Duration
  // 命名空间
	namespace   string
	lock        sync.Mutex
  // 缓存informer到map中
	informers   map[schema.GroupVersionResource]informers.GenericInformer
	// startInformers 用于跟踪哪些 Informers 已启动。这允许安全地多次调用 Start()。
	startedInformers map[schema.GroupVersionResource]bool
	tweakListOptions TweakListOptionsFunc
}

// dynamicSharedInformerFactory实际上实现了DynamicSharedInformerFactory
// 而DynamicSharedInformerFactory为动态客户端提供对共享informer和lister的访问
type DynamicSharedInformerFactory interface {
    // 启动所有informer的方法
	Start(stopCh <-chan struct{})
    // 获取Informer的方法
	ForResource(gvr schema.GroupVersionResource) informers.GenericInformer
    // 等待缓存同步的方法
	WaitForCacheSync(stopCh <-chan struct{}) map[schema.GroupVersionResource]bool
}

// 实现DynamicSharedInformerFactory的ForResource方法
func (f *dynamicSharedInformerFactory) ForResource(gvr schema.GroupVersionResource) informers.GenericInformer {
	f.lock.Lock()
	defer f.lock.Unlock()

	key := gvr
    // 获取缓存map中的informer
	informer, exists := f.informers[key]
	if exists {
		return informer
	}
    // 不存在就创建
	informer = NewFilteredDynamicInformer(f.client, gvr, f.namespace, f.defaultResync, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
	f.informers[key] = informer

	return informer
}

// 实现SharedInformerFactory的Start方法，启动所有未启动的informer。
func (f *dynamicSharedInformerFactory) Start(stopCh <-chan struct{}) {
	f.lock.Lock()
	defer f.lock.Unlock()

    // 遍历所有informer
	for informerType, informer := range f.informers {
        // 判断该informer是否已经启动
		if !f.startedInformers[informerType] {
            // 启动informer
			go informer.Informer().Run(stopCh)
            // 设置对应gvr的informer已经启动
			f.startedInformers[informerType] = true
		}
	}
}

// 实现SharedInformerFactory的WaitForCacheSync方法，等待所有启动的informer的缓存同步。
func (f *dynamicSharedInformerFactory) WaitForCacheSync(stopCh <-chan struct{}) map[schema.GroupVersionResource]bool {
	informers := func() map[schema.GroupVersionResource]cache.SharedIndexInformer {
		f.lock.Lock()
		defer f.lock.Unlock()
        // 定义map，用于接收所有已经启动的informer
		informers := map[schema.GroupVersionResource]cache.SharedIndexInformer{}
		for informerType, informer := range f.informers {
			if f.startedInformers[informerType] {
				informers[informerType] = informer.Informer()
			}
		}
		return informers
	}()
    // 定义map，用于接收所有同步完成的informer
	res := map[schema.GroupVersionResource]bool{}
    // 遍历已经启动的所有informer
	for informType, informer := range informers {
        // 执行同步方法
        // (1) 如果informer中controller为空，返回false，
        // (2) 如果informer.controller的queue还没有调用过Add/Update/Delete/AddIfNotPresent或者queue的initialPopulationCount != 0 (队列中还有数据)，返回false
		res[informType] = cache.WaitForCacheSync(stopCh, informer.HasSynced)
	}
	return res
}

// 动态Informer结构体，包装了SharedIndexInformer和gvr
type dynamicInformer struct {
	informer cache.SharedIndexInformer
	gvr      schema.GroupVersionResource
}

// 实现GenericInformer的Informer方法
func (d *dynamicInformer) Informer() cache.SharedIndexInformer {
	return d.informer
}

// 实现GenericInformer的Lister方法，使用dynamicInformer的indexer和gvr构造以Lister
func (d *dynamicInformer) Lister() cache.GenericLister {
	return dynamiclister.NewRuntimeObjectShim(dynamiclister.New(d.informer.GetIndexer(), d.gvr))
}
```

###  dynamiclister.Lister

```go
// dynamiclister.Lister
// Lister 获取资源和获取NamespaceLister。
type Lister interface {
	// List 列出索引器（缓存）中的所有资源。
	List(selector labels.Selector) (ret []*unstructured.Unstructured, err error)
	// Get 从索引器（缓存）中检索具有给定名称的资源
	Get(name string) (*unstructured.Unstructured, error)
	// Namespace 返回一个对象，该对象可以列出和获取给定命名空间中的资源。
	Namespace(namespace string) NamespaceLister
}

// 初始化Lister的方法:dynamiclister.New(informer.GetIndexer(), v1.VMGVR),实际实例化dynamicLister结构体
// New 返回一个新的 Lister.
func New(indexer cache.Indexer, gvr schema.GroupVersionResource) Lister {
	return &dynamicLister{indexer: indexer, gvr: gvr}
}

// dynamicLister 实现了 Lister 接口。
type dynamicLister struct {
  // 索引器（缓存）
  indexer cache.Indexer
  // 索引器对应的资源gvr，该索引器只存储该gvr对应的资源
  gvr     schema.GroupVersionResource
}

// dynamicLister一些常用方法
// Get 从索引器中检索具有给定名称的资源
func (l *dynamicLister) Get(name string) (*unstructured.Unstructured, error) {
  // 没有使用索引，这里为什么没有判断是否是l中对应的gvr？ 因为在add到indexer时已经做了区分，不同的gvr在不同的indexer中
  obj, exists, err := l.indexer.GetByKey(name)
  if err != nil {
	  return nil, err
  }
  if !exists {
	  return nil, errors.NewNotFound(l.gvr.GroupResource(), name)
  }
  return obj.(*unstructured.Unstructured), nil
}

// Namespace 返回一个对象，该对象可以从给定的命名空间中列出和获取资源.
func (l *dynamicLister) Namespace(namespace string) NamespaceLister {
	return &dynamicNamespaceLister{indexer: l.indexer, namespace: namespace, gvr: l.gvr}
}

// dynamicNamespaceLister 实现了 NamespaceLister 接口。相比dynamicLister多了namespace属性，用来限定namespace
type dynamicNamespaceLister struct {
  // 索引器（缓存）
  indexer   cache.Indexer
  // 命名空间
  namespace string
  // 索引器对应的资源gvr，该索引器只存储该gvr对应的资源
  gvr       schema.GroupVersionResource
}

// List 列出索引器中给定命名空间的所有资源。
func (l *dynamicNamespaceLister) List(selector labels.Selector) (ret []*unstructured.Unstructured, err error) {
  // 该方法到对应包在做具体分析，用来获取符合selector和namespace对应添加的item
  err = cache.ListAllByNamespace(l.indexer, l.namespace, selector, func(m interface{}) {
    	ret = append(ret, m.(*unstructured.Unstructured))
  })
  return ret, err
}

// Get 从索引器中检索给定命名空间和名称的资源。
func (l *dynamicNamespaceLister) Get(name string) (*unstructured.Unstructured, error) {
  // 注意： 这里可以看到indexer中items的存放,当namespace不为空时，key是${namespace}/${name}
  obj, exists, err := l.indexer.GetByKey(l.namespace + "/" + name)
  if err != nil {
	  return nil, err
  }
  if !exists {
	  return nil, errors.NewNotFound(l.gvr.GroupResource(), name)
  }
  return obj.(*unstructured.Unstructured), nil
}
```

### record.EventRecorder

```go
type EventRecorder interface {
   // 没有格式化字段的事件消息
   Event(object runtime.Object, eventtype, reason, message string)
   // Eventf类似于Event，但是消息字段使用了Sprintf。
   Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{})
   // AnnotatedEventf类似于eventf，但附加了注解
   AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{})
}
```

这里的三个方法都是记录事件用的，`Eventf` 就是封装了类似 `Printf` 的信息打印机制，内部也会调用 `Event`，而 `PastEventf` 允许用户传进来自定义的时间戳，因此可以设置事件产生的时间。