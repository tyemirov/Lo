package targetconfig

const (
	CPU                  = "cpu"
	Capability           = "Device.use"
	Mismatch             = "Tensor.DeviceMismatch"
	SimulationProfile    = "cuda-sim-fp32"
	CUDAProfile          = "cuda-fp32-pedantic"
	CUDA                 = "cuda:0"
	Simulation           = "cuda-sim:0"
	Metal                = "metal:0"
	Unavailable          = "Device.Unavailable"
	AllocationFailed     = "Device.AllocationFailed"
	TransferFailed       = "Device.TransferFailed"
	UnsupportedOperation = "Device.UnsupportedOperation"
	UnsupportedProfile   = "Device.UnsupportedProfile"
	LibraryFailed        = "Device.LibraryFailed"
	Lost                 = "Device.Lost"
	MetalProfile         = "metal-fp32"
	ExecutionFailed      = "Device.ExecutionFailed"
)
