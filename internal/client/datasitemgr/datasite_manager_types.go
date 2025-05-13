package datasitemgr

type DatasiteStatus string

const (
	DatasiteStatusUnprovisioned DatasiteStatus = "UNPROVISIONED"
	DatasiteStatusProvisioning  DatasiteStatus = "PROVISIONING"
	DatasiteStatusProvisioned   DatasiteStatus = "PROVISIONED"
	DatasiteStatusError         DatasiteStatus = "ERROR"
)

type DatasiteManagerStatus struct {
	Status DatasiteStatus
	Error  error
}
