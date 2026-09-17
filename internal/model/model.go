package model

import "time"

type OS string
type Arch string
type BackendKind string
type Isolation string

const (
	OSLinux   OS = "linux"
	OSWindows OS = "windows"
	OSMacOS   OS = "macos"

	ArchAMD64 Arch = "amd64"
	ArchARM64 Arch = "arm64"

	BackendDocker BackendKind = "docker"
	BackendKVM    BackendKind = "kvm"
	BackendHyperV BackendKind = "hyperv"
	BackendNative BackendKind = "native"

	IsolationContainer Isolation = "container"
	IsolationVM        Isolation = "vm"
	IsolationNative    Isolation = "native"
)

type Capacity struct {
	TotalCPU     int `json:"total_cpu"`
	FreeCPU      int `json:"free_cpu"`
	TotalMemoryM int `json:"total_memory_mb"`
	FreeMemoryM  int `json:"free_memory_mb"`
	RunningJobs  int `json:"running_jobs"`
}

type Node struct {
	ID           string        `json:"id"`
	OS           OS            `json:"os"`
	Arch         Arch          `json:"arch"`
	Backends     []BackendKind `json:"backends"`
	Capabilities []string      `json:"capabilities"`
	Labels       []string      `json:"labels,omitempty"`
	Capacity     Capacity      `json:"capacity"`
	Online       bool          `json:"online"`
	LastSeen     time.Time     `json:"last_seen"`
}

type JobRequirements struct {
	Repository   string    `json:"repository"`
	OS           OS        `json:"os"`
	Arch         Arch      `json:"arch"`
	Isolation    Isolation `json:"isolation"`
	Capabilities []string  `json:"capabilities,omitempty"`
	CPU          int       `json:"cpu,omitempty"`
	MemoryM      int       `json:"memory_mb,omitempty"`
}

type Placement struct {
	NodeID  string      `json:"node_id"`
	Backend BackendKind `json:"backend"`
}
