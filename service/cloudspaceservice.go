package service

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/mangohow/cloud-ide-k8s-controller/pb"
	"github.com/mangohow/cloud-ide-k8s-controller/tools/statussync"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ResponseSuccess = &pb.Response{Status: 200, Message: "success"}
	ResponseFailed  = &pb.Response{Status: 400, Message: "failed"}
)

const (
	PodNotExist int32 = iota
	PodExist
)

type CloudSpaceService struct {
	client         client.Client
	logger         *logr.Logger
	statusInformer *statussync.StatusInformer
}

func NewCloudSpaceService(client client.Client, logger *logr.Logger, manager *statussync.StatusInformer) *CloudSpaceService {
	return &CloudSpaceService{
		client:         client,
		logger:         logger,
		statusInformer: manager,
	}
}

// CreateSpaceAndWaitForRunning 创建一个云IDE空间, 并等待Pod的状态变为Running
func (s *CloudSpaceService) CreateSpaceAndWaitForRunning(ctx context.Context, info *pb.PodInfo) (*pb.PodSpaceInfo, error) {
	pod := podTpl.DeepCopy()
	s.fillPod(info, pod)
	err := s.client.Create(context.Background(), pod)
	if err != nil {
		fmt.Printf("create pod:%s, info:%v\n", err.Error(), info)
		return nil, err
	}
	// 向informer中添加chan，当Pod准备就绪时就会收到通知
	ch := s.statusInformer.Add(pod.Name)
	// 从informer中删除
	defer s.statusInformer.Delete(pod.Name)
	// 等待pod状态处于Running
	<-ch

	return s.GetPodSpaceInfo(context.Background(), &pb.QueryOption{
		Name:      info.Name,
		Namespace: info.Namespace,
	})
}

func (s *CloudSpaceService) fillPod(info *pb.PodInfo, pod *v1.Pod) {
	pod.Name = info.Name
	pod.Namespace = info.Namespace
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
			Resources: v1.ResourceRequirements{
				//Limits: map[v1.ResourceName]resource.Quantity{
				//	v1.ResourceCPU:    resource.MustParse(info.ResourceLimit.Cpu),
				//	v1.ResourceMemory: resource.MustParse(info.ResourceLimit.Memory),
				//},
				// 最小需求CPU2核、内存4Gi == 4 * 2^10
				//Requests: map[v1.ResourceName]resource.Quantity{
				//	v1.ResourceCPU:    resource.MustParse("2"),
				//	v1.ResourceMemory: resource.MustParse("4Gi"),
				//},
			},
		},
	}

}

// DeleteSpace 删除一个云空间
func (s *CloudSpaceService) DeleteSpace(ctx context.Context, option *pb.QueryOption) (*pb.Response, error) {
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      option.Name,
			Namespace: option.Namespace,
		},
	}
	err := s.client.Delete(context.Background(), &pod)
	if err != nil {
		s.logger.Error(err, "delete space")
		return ResponseFailed, err
	}

	return ResponseSuccess, nil
}

func (s *CloudSpaceService) GetPodSpaceStatus(ctx context.Context, option *pb.QueryOption) (*pb.PodStatus, error) {
	pod := v1.Pod{}
	err := s.client.Get(context.Background(), client.ObjectKey{Name: option.Name, Namespace: option.Namespace}, &pod)
	if err != nil {
		s.logger.Error(err, "get pod space status")
		return &pb.PodStatus{Status: PodNotExist, Message: "NotExist"}, err
	}

	return &pb.PodStatus{Status: PodExist, Message: string(pod.Status.Phase)}, nil
}

func (s *CloudSpaceService) GetPodSpaceInfo(ctx context.Context, option *pb.QueryOption) (*pb.PodSpaceInfo, error) {
	pod := v1.Pod{}
	err := s.client.Get(context.Background(), client.ObjectKey{Name: option.Name, Namespace: option.Namespace}, &pod)
	if err != nil {
		s.logger.Error(err, "get pod space info")
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
