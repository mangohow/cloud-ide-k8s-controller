package service

import (
	"context"
	"github.com/mangohow/cloud-ide-k8s-controller/pb"
	"github.com/mangohow/cloud-ide-k8s-controller/tools/statussync"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

/*
	由于K8S无法停止(Stop)正在运行的Pod,因此想要停止一个Pod的运行,必须删除该Pod
	如果后续需要继续运行该Pod,就需要重新创建,那么就会存在下面的问题：
	Pod的运行状态(Pod中产生的数据)无法保存,如果里面运行的是Code-Server,
	那么不光用户的代码数据无法保存,而且用户安装的插件和软件在Pod被删除时被销毁,
	因此,如果要实现保存用户的数据,就需要将使用存储卷(PV,PVC;nfs)
	在创建工作空间后第一次启动,需要将Code-Server的插件以及配置数据复制到存储卷中,
	并且修改Code-Server的插件保存位置(默认为/root/.local中)
	在后续的启动中就无需再次复制了(可以解决用户数据和Code-Server插件的保存,用户安装的程序在工作空间重新启动后就会消失)
*/

var Mode string

const ModeRelease = "release"

var (
	ResponseSuccess = &pb.Response{Status: 200, Message: "success"}
	ResponseFailed  = &pb.Response{Status: 400, Message: "failed"}
)

const (
	PodNotExist int32 = iota
	PodExist
)

var _ pb.CloudIdeServiceServer = &CloudSpaceService{}

type CloudSpaceService struct {
	client         client.Client
	statusInformer *statussync.StatusInformer
}

func NewCloudSpaceService(client client.Client, manager *statussync.StatusInformer) *CloudSpaceService {
	return &CloudSpaceService{
		client:         client,
		statusInformer: manager,
	}
}

// CreateSpace 创建云IDE空间并等待Pod状态变为Running,第一次创建,需要挂载存储卷
func (s *CloudSpaceService) CreateSpace(ctx context.Context, info *pb.PodInfo) (*pb.PodSpaceInfo, error) {
	// 1. 创建pvc,pvc的name和pod相同
	pvcName := info.Name
	pvc, err := s.constructPVC(pvcName, info.Namespace, info.ResourceLimit.Storage)
	if err != nil {
		klog.Errorf("construct pvc error:%v, info:%v", err, info)
		return nil, ErrConstructPVC
	}
	klog.Infof("[CreateSpace] 1.construct pvc")
	deadline, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	err = s.client.Create(deadline, pvc)
	if err != nil {
		// 如果PVC已经存在
		if errors.IsAlreadyExists(err) {
			klog.Infof("create pvc while pvc is already exist, pvc:%s", pvcName)
		} else {
			klog.Errorf("create pvc error:%v", err)
			return nil, ErrCreatePVC
		}
	}
	klog.Info("[CreateSpace] 2.create pvc success")

	// 2.创建Pod
	return s.createPod(ctx, info)
}

func (s *CloudSpaceService) createPod(c context.Context, info *pb.PodInfo) (*pb.PodSpaceInfo, error) {
	pod := podTpl.DeepCopy()
	s.fillPod(info, pod, info.Name)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	err := s.client.Create(ctx, pod)
	if err != nil {
		// 如果该Pod已经存在
		if errors.IsAlreadyExists(err) {
			klog.Infof("create pod while pod is already exist, pod:%s", info.Name)
			// 判断Pod是否处于running状态
			existPod := v1.Pod{}
			err = s.client.Get(context.Background(), client.ObjectKeyFromObject(pod), &existPod)
			if err != nil {
				return nil, ErrCreatePod
			}
			if existPod.Status.Phase == v1.PodRunning {
				return &pb.PodSpaceInfo{
					NodeName: existPod.Spec.NodeName,
					Ip:       existPod.Status.PodIP,
					Port:     existPod.Spec.Containers[0].Ports[0].ContainerPort,
				}, nil
			} else {
				s.deletePod(&existPod)
				return nil, ErrCreatePod
			}

		} else {
			klog.Errorf("create pod err:%v", err)
			return nil, ErrCreatePod
		}
	}

	klog.Info("[createPod] create pod success")
	// 向informer中添加chan，当Pod准备就绪时就会收到通知
	ch := s.statusInformer.Add(pod.Name)
	// 从informer中删除
	defer s.statusInformer.Delete(pod.Name)

	select {
	// 等待pod状态处于Running
	case <-ch:
		// Pod已经处于running状态
		return s.GetPodSpaceInfo(context.Background(), &pb.QueryOption{Name: info.Name, Namespace: info.Namespace})
	case <-c.Done():
		// 超时,Pod启动失败,可能是由于资源不足,将Pod删除
		klog.Error("pod start failed, maybe resources is not enough")
		s.deletePod(pod)
		return nil, ErrCreatePod
	}

}

/*
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc1
  namespace: cloud-ide
spec:
  accessModes: # 访客模式
    - ReadWriteMany
  resources: # 请求空间
    requests:
      storage: 5Gi
*/

// 构造PVC
func (s *CloudSpaceService) constructPVC(name, namespace, storage string) (*v1.PersistentVolumeClaim, error) {
	quantity, err := resource.ParseQuantity(storage)
	if err != nil {
		return nil, err
	}

	pvc := &v1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			Resources: v1.ResourceRequirements{
				Limits:   v1.ResourceList{v1.ResourceStorage: quantity},
				Requests: v1.ResourceList{v1.ResourceStorage: quantity},
			},
		},
	}

	return pvc, nil
}

/*
apiVersion: v1
kind: Pod
metadata:
  name: code-server-volum
  namespace: cloud-ide
  labels:
    kind: code-server
spec:
  containers:
  - name: code-server
    image: mangohow/code-server-go1.19:v0.1
    volumeMounts:
    - name: volume
      mountPath: /root/workspace
  volumes:
  - name: volume
    persistentVolumeClaim:
      claimName: pvc3
      readOnly: false
*/

func (s *CloudSpaceService) fillPod(info *pb.PodInfo, pod *v1.Pod, pvc string) {
	volumeName := "volume-user-workspace"
	pod.Name = info.Name
	pod.Namespace = info.Namespace
	// 配置持久化存储
	pod.Spec.Volumes = []v1.Volume{
		v1.Volume{
			Name: volumeName,
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvc,
					ReadOnly:  false,
				},
			},
		},
	}
	pod.Spec.Containers = []v1.Container{
		{
			Name:            info.Name,
			Image:           info.Image,
			ImagePullPolicy: v1.PullIfNotPresent,
			Ports: []v1.ContainerPort{
				{
					ContainerPort: int32(info.Port),
				},
			},
			// 容器挂载存储卷
			VolumeMounts: []v1.VolumeMount{
				v1.VolumeMount{
					Name:      volumeName,
					ReadOnly:  false,
					MountPath: "/user_data/",
				},
			},
		},
	}

	if Mode == ModeRelease {
		// 最小需求CPU2核、内存1Gi == 1 * 2^10
		pod.Spec.Containers[0].Resources = v1.ResourceRequirements{
			Requests: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse(info.ResourceLimit.Cpu),
				v1.ResourceMemory: resource.MustParse(info.ResourceLimit.Memory),
			},
		}
	}

}

// StartSpace 启动(创建)云IDE空间,非第一次创建,无需挂载存储卷,使用之前的存储卷
func (s *CloudSpaceService) StartSpace(ctx context.Context, info *pb.PodInfo) (*pb.PodSpaceInfo, error) {
	return s.createPod(ctx, info)
}

// DeleteSpace 删除云IDE空间, 只需要删除存储卷
func (s *CloudSpaceService) DeleteSpace(ctx context.Context, option *pb.QueryOption) (*pb.Response, error) {
	// 删除pvc
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      option.Name,
			Namespace: option.Namespace,
		},
	}
	c, cancelFunc := context.WithTimeout(context.Background(), time.Second*30)
	defer cancelFunc()
	err := s.client.Delete(c, pvc)
	if err != nil {
		// 如果是PVC不存在引起的错误就认为是成功了,因为就是要删除PVC
		if errors.IsNotFound(err) {
			klog.Infof("pvc not found,err:%v", err)
			return ResponseSuccess, nil
		}
		klog.Errorf("delete pvc error:%v", err)
		return ResponseFailed, ErrDeletePVC
	}
	klog.Info("[DeleteSpace] delete pvc success")

	return ResponseSuccess, nil
}

func (s *CloudSpaceService) deletePod(pod *v1.Pod) (*pb.Response, error) {
	// k8s的默认最大宽限时间为30s,因此在这设置为32s
	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*32)
	defer cancelFunc()
	err := s.client.Delete(ctx, pod)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("delete pod while pod not exist, pod:%s", pod.Name)
			return ResponseSuccess, nil
		}

		klog.Errorf("delete pod error:%v", err)
		return ResponseFailed, ErrDeletePod
	}
	klog.Info("[deletePod] delete pod success")

	return ResponseSuccess, nil
}

// StopSpace 停止(删除)云工作空间,无需删除存储卷
func (s *CloudSpaceService) StopSpace(ctx context.Context, option *pb.QueryOption) (*pb.Response, error) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      option.Name,
			Namespace: option.Namespace,
		},
	}

	return s.deletePod(pod)
}

// GetPodSpaceStatus 获取Pod运行状态
func (s *CloudSpaceService) GetPodSpaceStatus(ctx context.Context, option *pb.QueryOption) (*pb.PodStatus, error) {
	pod := v1.Pod{}
	err := s.client.Get(ctx, client.ObjectKey{Name: option.Name, Namespace: option.Namespace}, &pod)
	if err != nil {
		klog.Errorf("get pod space status error:%v", err)
		return &pb.PodStatus{Status: PodNotExist, Message: "NotExist"}, err
	}

	return &pb.PodStatus{Status: PodExist, Message: string(pod.Status.Phase)}, nil
}

// GetPodSpaceInfo 获取云IDE空间Pod的信息
func (s *CloudSpaceService) GetPodSpaceInfo(ctx context.Context, option *pb.QueryOption) (*pb.PodSpaceInfo, error) {
	pod := v1.Pod{}
	err := s.client.Get(ctx, client.ObjectKey{Name: option.Name, Namespace: option.Namespace}, &pod)
	if err != nil {
		klog.Errorf("get pod space info error:%v", err)
		return &pb.PodSpaceInfo{}, err
	}

	return &pb.PodSpaceInfo{NodeName: pod.Spec.NodeName,
		Ip:   pod.Status.PodIP,
		Port: pod.Spec.Containers[0].Ports[0].ContainerPort}, nil
}

var podTpl = &v1.Pod{
	TypeMeta: metav1.TypeMeta{
		Kind:       "Pod",
		APIVersion: "v1",
	},
	ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{
			"kind": "cloud-ide",
		},
	},
}
