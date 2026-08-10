package base

const (
	CstTypeOPAllocate = "opAllocate" // 模型部署
	CstTypeOPRelease  = "opRelease"  // 模型销毁（释放）
	CstTypeOpScaling  = "opScaling"  // 节点伸缩

	CstTypeResourceDynamic = "dynamic"
	CstTypeResourceStatic  = "static"

	CstCategoryResourceMultiCard = "MultiCard" //多卡
	CstCategoryResourceGSE       = "GSE"
	CstCategoryResourceMPS       = "MPS"
	CstCategoryResourceMIG       = "MIG"
	CstCategoryResourceGeneral   = "General" // 通用k8s容器
	CstCategoryResourceVCUDA     = "vCUDA"   //cuda层虚拟GPU
	CstCategoryResourceVDriver   = "vGPU"    //驱动层虚拟GPU

	CstGseGpuMemResourceName     = "aliyun.com/gpu-mem"
	CstGeneralGpuMemResourceName = "nvidia.com/gpu"

	CstGpuVcudaCore           = "vesta.nvidia/vcuda-core"
	CstGpuVucdaMemory         = "vesta.nvidia/vcuda-memory"
	CstGpuVcudaDeployStrategy = "vesta.nvidia/gpu-deploy-strategy"
	CstStrategyDisperse       = "disperse"

	// env
	CstEnvPodName      = "POD_NAME"
	CstEnvPodNameValue = "metadata.name"
	CstEnvPodUid       = "POD_UID"
	CstEnvPodUidValue  = "metadata.uid"

	// nodeSelector
	CstGpuSeriesName = "gpu-series-name" // 显卡型号,分配pod的时候根据label选择对应的显卡型号
)

var (
	// mount
	// vcuda virtaul manager挂载
	CstVolumeMountVmMountPath       = "/etc/vcuda" // virtual manager path
	CstVolumeMountVmName            = "vcuda-config"
	CstVolumeMountSubPathExprPodUid = "$(POD_UID)"

	// vcuda监控挂载
	CstVolumeMountVgpuMetricsName      = "vgpu-metrics"
	CstVolumeMountVgpuMetricsMountPath = "/etc/vgpu-metrics"
)
