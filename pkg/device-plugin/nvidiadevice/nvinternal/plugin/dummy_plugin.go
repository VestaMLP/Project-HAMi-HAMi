package plugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/watch"
	"github.com/fsnotify/fsnotify"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	DummyCoreResourceName   = "yxqiche.com/vcuda-core"
	DummyMemoryResourceName = "yxqiche.com/vcuda-memory"
	DummyDeviceMultiplier   = 100 // 每个GPU乘以100
)

func GetDummyDeviceMultiplier() int {
	return DummyDeviceMultiplier
}

type DummyDevicePlugin struct {
	kubeletdevicepluginv1beta1.UnimplementedDevicePluginServer

	resourceName string
	socket       string
	server       *grpc.Server
	stop         chan any
	stopOnce     sync.Once
	deviceCount  int
	isRunning    bool
	runningMutex sync.RWMutex
}

func NewDummyDevicePlugin(resourceName string, deviceCount int) *DummyDevicePlugin {
	_, name := spec.ResourceName(resourceName).Split()
	pluginName := "hami-dummy-" + name

	klog.Infof("Creating Dummy device plugin for '%s' with %d devices",
		resourceName, deviceCount)

	return &DummyDevicePlugin{
		resourceName: resourceName,
		socket:       filepath.Join(kubeletdevicepluginv1beta1.DevicePluginPath, pluginName) + ".sock",
		stop:         make(chan any),
		deviceCount:  deviceCount,
		isRunning:    false,
	}
}

func (p *DummyDevicePlugin) Devices() rm.Devices {
	devices := make(rm.Devices)
	for i := 0; i < p.deviceCount; i++ {
		deviceID := fmt.Sprintf("dummy-%s-%d", p.resourceName, i)
		devices[deviceID] = &rm.Device{
			Device: kubeletdevicepluginv1beta1.Device{
				ID:     deviceID,
				Health: kubeletdevicepluginv1beta1.Healthy,
			},
		}
	}
	return devices
}

func (p *DummyDevicePlugin) Start(kubeletSocket string) error {
	p.runningMutex.Lock()
	if p.isRunning {
		p.runningMutex.Unlock()
		klog.Infof("Dummy device plugin for '%s' is already running", p.resourceName)
		return nil
	}
	p.runningMutex.Unlock()

	klog.Infof("Starting Dummy device plugin for resource '%s' with %d devices", p.resourceName, p.deviceCount)

	err := p.cleanup()
	if err != nil {
		return fmt.Errorf("failed to cleanup socket: %v", err)
	}

	listener, err := net.Listen("unix", p.socket)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %v", p.socket, err)
	}

	p.server = grpc.NewServer([]grpc.ServerOption{
		grpc.Creds(insecure.NewCredentials()),
	}...)
	kubeletdevicepluginv1beta1.RegisterDevicePluginServer(p.server, p)

	go func() {
		if err := p.server.Serve(listener); err != nil {
			klog.Errorf("Dummy device plugin server for '%s' error: %v", p.resourceName, err)
		}
	}()

	klog.Infof("Starting to serve Dummy device plugin for '%s' on %s", p.resourceName, p.socket)

	time.Sleep(1 * time.Second)

	maxRetries := 5
	retryInterval := 2 * time.Second
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		err = p.Register(kubeletSocket)
		if err == nil {
			break
		}
		lastErr = err
		klog.Warningf("Failed to register Dummy device plugin '%s' (attempt %d/%d): %v. Retrying in %v...",
			p.resourceName, i+1, maxRetries, err, retryInterval)
		time.Sleep(retryInterval)
		retryInterval *= 2 // 指数退避
	}

	if err != nil {
		p.Stop()
		return fmt.Errorf("failed to register Dummy device plugin after %d attempts: %v (last error: %v)",
			maxRetries, lastErr, err)
	}

	p.runningMutex.Lock()
	p.isRunning = true
	p.runningMutex.Unlock()

	klog.Infof("Registered Dummy device plugin for '%s' with Kubelet (%d devices)", p.resourceName, p.deviceCount)

	go p.watchAndReregister()

	return nil
}

func (p *DummyDevicePlugin) Stop() error {
	p.stopOnce.Do(func() {
		klog.Infof("Stopping Dummy device plugin for '%s'", p.resourceName)
		close(p.stop)
	})

	p.runningMutex.Lock()
	p.isRunning = false
	p.runningMutex.Unlock()

	if p.server != nil {
		p.server.Stop()
	}
	return p.cleanup()
}

func (p *DummyDevicePlugin) Register(kubeletSocket string) error {
	if kubeletSocket == "" {
		klog.Info("Skipping registration with Kubelet for Dummy plugin")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, kubeletSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to kubelet: %v", err)
	}
	defer conn.Close()

	client := kubeletdevicepluginv1beta1.NewRegistrationClient(conn)
	reqt := &kubeletdevicepluginv1beta1.RegisterRequest{
		Version:      kubeletdevicepluginv1beta1.Version,
		Endpoint:     filepath.Base(p.socket),
		ResourceName: p.resourceName,
		Options: &kubeletdevicepluginv1beta1.DevicePluginOptions{
			GetPreferredAllocationAvailable: false,
			PreStartRequired:                false,
		},
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return fmt.Errorf("failed to register with kubelet: %v", err)
	}

	klog.Infof("Successfully registered Dummy device plugin '%s' with Kubelet", p.resourceName)

	return nil
}

func (p *DummyDevicePlugin) GetDevicePluginOptions(context.Context, *kubeletdevicepluginv1beta1.Empty) (*kubeletdevicepluginv1beta1.DevicePluginOptions, error) {
	return &kubeletdevicepluginv1beta1.DevicePluginOptions{
		GetPreferredAllocationAvailable: false,
		PreStartRequired:                false,
	}, nil
}

func (p *DummyDevicePlugin) ListAndWatch(e *kubeletdevicepluginv1beta1.Empty, s kubeletdevicepluginv1beta1.DevicePlugin_ListAndWatchServer) error {
	klog.InfoS("Dummy device plugin ListAndWatch starting", "resource", p.resourceName, "devices", p.deviceCount)

	devices := make([]*kubeletdevicepluginv1beta1.Device, 0, p.deviceCount)
	for i := 0; i < p.deviceCount; i++ {
		deviceID := fmt.Sprintf("dummy-%s-%d", p.resourceName, i)
		devices = append(devices, &kubeletdevicepluginv1beta1.Device{
			ID:     deviceID,
			Health: kubeletdevicepluginv1beta1.Healthy,
		})
	}

	err := s.Send(&kubeletdevicepluginv1beta1.ListAndWatchResponse{Devices: devices})
	if err != nil {
		return fmt.Errorf("failed to send initial device list: %v", err)
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stop:
			klog.InfoS("Dummy device plugin ListAndWatch stopping", "resource", p.resourceName)
			return nil
		case <-ticker.C:
			err := s.Send(&kubeletdevicepluginv1beta1.ListAndWatchResponse{Devices: devices})
			if err != nil {
				return fmt.Errorf("failed to send device list update: %v", err)
			}
		}
	}
}

func (p *DummyDevicePlugin) Allocate(ctx context.Context, reqs *kubeletdevicepluginv1beta1.AllocateRequest) (*kubeletdevicepluginv1beta1.AllocateResponse, error) {
	klog.InfoS("Dummy device plugin Allocate called - returning empty response",
		"resource", p.resourceName,
		"containers", len(reqs.ContainerRequests))

	responses := &kubeletdevicepluginv1beta1.AllocateResponse{}
	for range reqs.ContainerRequests {
		responses.ContainerResponses = append(responses.ContainerResponses, &kubeletdevicepluginv1beta1.ContainerAllocateResponse{})
	}

	return responses, nil
}

func (p *DummyDevicePlugin) PreStartContainer(context.Context, *kubeletdevicepluginv1beta1.PreStartContainerRequest) (*kubeletdevicepluginv1beta1.PreStartContainerResponse, error) {
	return &kubeletdevicepluginv1beta1.PreStartContainerResponse{}, nil
}

func (p *DummyDevicePlugin) watchAndReregister() {
	kubeletSocket := kubeletdevicepluginv1beta1.KubeletSocket
	watcher, err := watch.Files(kubeletSocket)
	if err != nil {
		klog.Errorf("Failed to create FS watcher for Dummy plugin '%s': %v", p.resourceName, err)
		return
	}
	defer watcher.Close()

	for {
		select {
		case <-p.stop:
			return
		case event := <-watcher.Events:
			if event.Name == kubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				klog.InfoS("Kubelet restarted, re-registering Dummy plugin", "resource", p.resourceName)
				time.Sleep(5 * time.Second)
				err := p.Register(kubeletSocket)
				if err != nil {
					klog.Errorf("Failed to re-register Dummy plugin '%s': %v", p.resourceName, err)
				} else {
					klog.InfoS("Successfully re-registered Dummy plugin", "resource", p.resourceName)
				}
			}
		case err := <-watcher.Errors:
			klog.Errorf("FS watcher error for Dummy plugin '%s': %v", p.resourceName, err)
		}
	}
}

func (p *DummyDevicePlugin) cleanup() error {
	if _, err := os.Stat(p.socket); err == nil {
		if err := os.Remove(p.socket); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove socket file %s: %v", p.socket, err)
		}
	}
	return nil
}
