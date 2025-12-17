// Package model 提供数据库模型定义
package model

// NetInterface 网络接口资产模型
type NetInterface struct {
	ID            string      `gorm:"primaryKey;column:id;type:varchar(64);not null" json:"id"`
	HostID        string      `gorm:"column:host_id;type:varchar(64);not null;index" json:"host_id"`
	InterfaceName string      `gorm:"column:interface_name;type:varchar(50);not null" json:"interface_name"` // eth0、ens33 等
	MACAddress    string      `gorm:"column:mac_address;type:varchar(20)" json:"mac_address"`
	IPv4Addresses StringArray `gorm:"column:ipv4_addresses;type:text" json:"ipv4_addresses"` // JSON 数组
	IPv6Addresses StringArray `gorm:"column:ipv6_addresses;type:text" json:"ipv6_addresses"` // JSON 数组
	MTU           int         `gorm:"column:mtu;type:int" json:"mtu"`
	State         string      `gorm:"column:state;type:varchar(20)" json:"state"` // up、down
	CollectedAt   LocalTime   `gorm:"column:collected_at;type:timestamp;not null;index" json:"collected_at"`
}

// TableName 指定表名
func (NetInterface) TableName() string {
	return "network_interfaces"
}
