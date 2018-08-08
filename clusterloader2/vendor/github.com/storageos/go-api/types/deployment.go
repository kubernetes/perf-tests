package types

import "time"

// Deployment Volume master or replica deployment details.
// swagger:model Deployment
type Deployment struct {

	// Deployment unique ID
	// Read Only: true
	ID string `json:"id"`

	// Inode number
	// Read Only: true
	Inode uint32 `json:"inode"`

	// Node ID
	// Read Only: true
	Node string `json:"node"`

	// Node name
	// Read Only: true
	NodeName string `json:"nodeName"`

	// Controller ID
	// Read Only: true
	// DEPRECATED: remove in 0.11
	Controller string `json:"controller"`

	// Controller name
	// Read Only: true
	// DEPRECATED: remove in 0.11
	ControllerName string `json:"controllerName"`

	// Health
	// Read Only: true
	Health string `json:"health"`

	// Status
	// Read Only: true
	Status string `json:"status"`

	// Created at
	// Read Only: true
	CreatedAt time.Time `json:"createdAt"`
}
