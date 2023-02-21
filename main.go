/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"github.com/mangohow/cloud-ide-k8s-controller/middleware"
	"github.com/mangohow/cloud-ide-k8s-controller/pb"
	"github.com/mangohow/cloud-ide-k8s-controller/service"
	"github.com/mangohow/cloud-ide-k8s-controller/tools/signal"
	"github.com/mangohow/cloud-ide-k8s-controller/tools/statussync"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	"net"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/mangohow/cloud-ide-k8s-controller/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const WatchedNamespace = "cloud-ide"

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&service.Mode, "mode", service.ModeRelease, "The program running mode(debug or release)")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	fmt.Printf("Running mode:%s\n", service.Mode)

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "81275557.my.domain",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the StatusInformer ends. This requires the binary to immediately end when the
		// StatusInformer is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
		Namespace: WatchedNamespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	manager := statussync.NewManager()
	if err = controllers.NewPodReconciler(mgr.GetClient(), mgr.GetScheme(), manager).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// 启动grpc服务
	grpcServer := StartGrpcServer(mgr.GetClient(), manager)
	// 安装信号处理
	ctx := signal.SetupSignal(func() {
		ctrl.Log.Info("receive signal, is going to shutdown")
		grpcServer.GracefulStop()
	})

	setupLog.Info("starting manager")

	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func StartGrpcServer(client client.Client, manager *statussync.StatusInformer) *grpc.Server {
	listener, err := net.Listen("tcp", ":6387")
	if err != nil {
		panic(fmt.Errorf("create grpc service: %v", err))
	}
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(
		middleware.RecoveryInterceptorMiddleware(),
		middleware.LogInterceptorMiddleware(),
	))
	pb.RegisterCloudIdeServiceServer(server, service.NewCloudSpaceService(client, manager))

	go func() {
		err := server.Serve(listener)
		if err != nil && err == grpc.ErrServerStopped {
			klog.Info("server stopped")
		} else if err != nil {
			panic(fmt.Errorf("start grpc server: %v", err))
		}
	}()

	return server
}
