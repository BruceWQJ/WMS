package model

import "encoding/json"

// Warehouse 描述仓库网格与出入口配置。
type Warehouse struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	StartX int    `json:"startX"`
	StartY int    `json:"startY"`
	EndX   int    `json:"endX"`
	EndY   int    `json:"endY"`
}

// Location 描述一个库位的坐标、类型、巷道属性与库存。
type Location struct {
	ID              int64          `json:"id"`
	X               int            `json:"x"`
	Y               int            `json:"y"`
	StorageType     string         `json:"storageType"`
	AisleID         string         `json:"aisleId"`
	Direction       string         `json:"direction"`
	PickingPriority int            `json:"pickingPriority"`
	Inventory       map[string]int `json:"inventory"`
}

// UnmarshalJSON 同时兼容驼峰与下划线两种 JSON 字段命名。
func (l *Location) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID               int64          `json:"id"`
		X                int            `json:"x"`
		Y                int            `json:"y"`
		StorageType      string         `json:"storageType"`
		StorageTypeSnake string         `json:"storage_type"`
		AisleID          string         `json:"aisleId"`
		AisleIDSnake     string         `json:"aisle_id"`
		Direction        string         `json:"direction"`
		PickingPriority  *int           `json:"pickingPriority"`
		PrioritySnake    *int           `json:"picking_priority"`
		Inventory        map[string]int `json:"inventory"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	l.ID = raw.ID
	l.X = raw.X
	l.Y = raw.Y
	l.StorageType = raw.StorageType
	if raw.StorageTypeSnake != "" {
		l.StorageType = raw.StorageTypeSnake
	}
	l.AisleID = raw.AisleID
	if raw.AisleIDSnake != "" {
		l.AisleID = raw.AisleIDSnake
	}
	l.Direction = raw.Direction
	l.Inventory = raw.Inventory
	l.PickingPriority = 0
	if raw.PickingPriority != nil {
		l.PickingPriority = *raw.PickingPriority
	}
	if raw.PrioritySnake != nil {
		l.PickingPriority = *raw.PrioritySnake
	}
	return nil
}

// InventoryItem 描述某库位上单个 SKU 的库存数量。
type InventoryItem struct {
	LocationID int64  `json:"locationId"`
	SKU        string `json:"sku"`
	Quantity   int    `json:"quantity"`
}

// OrderItemInput 是下单请求中的一行明细。
type OrderItemInput struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// CreateOrderRequest 是提交拣料单的请求体。
type CreateOrderRequest struct {
	Items []OrderItemInput `json:"items"`
}

// Allocation 记录一次订单中从某个库位扣减的明细。
type Allocation struct {
	LocationID int64  `json:"locationId"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	SKU        string `json:"sku"`
	Qty        int    `json:"qty"`
	Priority   int    `json:"priority"`
}

// Shortage 记录库存不足的 SKU 及其缺口。
type Shortage struct {
	SKU       string `json:"sku"`
	Requested int    `json:"requested"`
	Allocated int    `json:"allocated"`
}

// Point 是网格上的一个坐标点。
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Route 是动线算法的输出结果。
type Route struct {
	Path      []Point `json:"path"`
	Stops     []Point `json:"stops"`
	Distance  int     `json:"distance"`
	Reachable bool    `json:"reachable"`
	Message   string  `json:"message,omitempty"`
}

// OrderResponse 是下单与订单详情的响应体。
type OrderResponse struct {
	ID          int64        `json:"id"`
	Status      string       `json:"status"`
	Allocations []Allocation `json:"allocations"`
	Shortages   []Shortage   `json:"shortages"`
	Route       Route        `json:"route"`
	CreatedAt   string       `json:"createdAt"`
}
