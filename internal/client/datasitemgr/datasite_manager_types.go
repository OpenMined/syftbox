package datasitemgr

import "github.com/openmined/syftbox/internal/client/datasite"

type DatasiteStatus string

const (
	DatasiteStatusUnprovisioned DatasiteStatus = "UNPROVISIONED"
	DatasiteStatusProvisioning  DatasiteStatus = "PROVISIONING"
	DatasiteStatusProvisioned   DatasiteStatus = "PROVISIONED"
	DatasiteStatusError         DatasiteStatus = "ERROR"
)

// DatasiteManagerStatus represents the status of the datasite manager
type DatasiteManagerStatus struct {
	Status        DatasiteStatus     // status of the datasite manager
	DatasiteError error              // error that occurred while provisioning the datasite
	Datasite      *datasite.Datasite // datasite instance. available if status is PROVISIONED
}
