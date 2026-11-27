// main 是 WMS 服务入口：加载配置、初始化数据库、写入示例数据并启动 HTTP 服务。
package main

import (
	"log"
	"net/http"

	"wms/backend/internal/config"
	"wms/backend/internal/httpapi"
	"wms/backend/internal/store"
)

// main 装配各依赖并监听端口；数据库或路由初始化失败时直接退出。
func main() {
	cfg := config.Load()
	db, err := store.OpenDB(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	defer db.Close()

	var locationCount, orderCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM location`).Scan(&locationCount); err != nil {
		log.Fatalf("读取库位数量失败: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM orders`).Scan(&orderCount); err != nil {
		log.Fatalf("读取订单数量失败: %v", err)
	}
	if locationCount == 0 && orderCount == 0 {
		if _, err := store.SeedWarehouse(db); err != nil {
			log.Printf("初始化示例仓库失败: %v", err)
		} else {
			log.Printf("已生成示例仓库数据")
		}
	}

	handler := httpapi.New(cfg, db)
	log.Printf("WMS 服务已启动: http://localhost%s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, handler); err != nil {
		log.Fatal(err)
	}
}
