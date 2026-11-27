package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wms/backend/internal/config"
	"wms/backend/internal/model"
	"wms/backend/internal/service"
	"wms/backend/internal/store"
)

// App 持有 HTTP 处理器所需的依赖。
type App struct {
	db  *sql.DB
	cfg config.Config
}

// New 组装路由并返回可直接挂载的 http.Handler。
func New(cfg config.Config, db *sql.DB) http.Handler {
	a := &App{db: db, cfg: cfg}
	return withCORS(a.routes())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /api/warehouse", a.handleGetWarehouse)
	mux.HandleFunc("POST /api/warehouse", a.handlePostWarehouse)
	mux.HandleFunc("PUT /api/warehouse", a.handlePutWarehouse)
	mux.HandleFunc("DELETE /api/warehouse", a.handleDeleteWarehouse)

	mux.HandleFunc("GET /api/locations", a.handleListLocations)
	mux.HandleFunc("GET /api/locations/{id}", a.handleGetLocation)
	mux.HandleFunc("POST /api/locations", a.handleCreateLocation)
	mux.HandleFunc("PUT /api/locations/{id}", a.handleUpdateLocation)
	mux.HandleFunc("DELETE /api/locations/{id}", a.handleDeleteLocation)

	mux.HandleFunc("GET /api/inventory", a.handleListInventory)
	mux.HandleFunc("POST /api/inventory", a.handleSetInventory)

	mux.HandleFunc("POST /api/seed", a.handleSeed)

	mux.HandleFunc("GET /api/orders", a.handleListOrders)
	mux.HandleFunc("POST /api/orders", a.handleCreateOrder)
	mux.HandleFunc("GET /api/orders/{id}", a.handleGetOrder)

	spa := newSPAHandler(a.cfg.StaticDir)
	mux.Handle("/", spa)

	return mux
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleGetWarehouse(w http.ResponseWriter, r *http.Request) {
	wh, err := store.GetWarehouse(a.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wh)
}

func (a *App) handlePostWarehouse(w http.ResponseWriter, r *http.Request) {
	var wh model.Warehouse
	if err := json.NewDecoder(r.Body).Decode(&wh); err != nil {
		writeErr(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	created, err := store.CreateWarehouse(a.db, wh)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, created)
}

func (a *App) handleDeleteWarehouse(w http.ResponseWriter, r *http.Request) {
	reset, err := store.ResetWarehouse(a.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reset)
}

func (a *App) handlePutWarehouse(w http.ResponseWriter, r *http.Request) {
	var wh model.Warehouse
	if err := json.NewDecoder(r.Body).Decode(&wh); err != nil {
		writeErr(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	updated, err := store.UpdateWarehouse(a.db, wh)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *App) handleListLocations(w http.ResponseWriter, r *http.Request) {
	locs, err := store.ListLocations(a.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, locs)
}

func (a *App) handleGetLocation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效的库位 ID")
		return
	}
	loc, err := store.GetLocation(a.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "库位不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, loc)
}

func (a *App) handleCreateLocation(w http.ResponseWriter, r *http.Request) {
	var loc model.Location
	if err := json.NewDecoder(r.Body).Decode(&loc); err != nil {
		writeErr(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	loc.ID = 0
	saved, err := store.SaveLocation(a.db, loc)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (a *App) handleUpdateLocation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效的库位 ID")
		return
	}
	var loc model.Location
	if err := json.NewDecoder(r.Body).Decode(&loc); err != nil {
		writeErr(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	loc.ID = id
	saved, err := store.SaveLocation(a.db, loc)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "库位不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (a *App) handleDeleteLocation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效的库位 ID")
		return
	}
	if err := store.DeleteLocation(a.db, id); errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "库位不存在")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleListInventory(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListInventory(a.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *App) handleSetInventory(w http.ResponseWriter, r *http.Request) {
	var item model.InventoryItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeErr(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	if err := store.SetInventory(a.db, item); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) handleSeed(w http.ResponseWriter, r *http.Request) {
	wh, err := store.SeedWarehouse(a.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	locs, err := store.ListLocations(a.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"warehouse": wh, "locations": locs})
}

func (a *App) handleListOrders(w http.ResponseWriter, r *http.Request) {
	list, err := service.ListOrders(a.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *App) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req model.CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	wh, err := store.GetWarehouse(a.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp, err := service.CreateOrder(a.db, req, wh)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效的订单 ID")
		return
	}
	resp, err := service.GetOrder(a.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "订单不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// spaHandler 托管前端构建产物，并对非静态资源路径回退到 index.html。
type spaHandler struct {
	dir string
	fs  http.Handler
}

func newSPAHandler(dir string) http.Handler {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return &spaHandler{dir: abs, fs: http.FileServer(http.Dir(abs))}
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if info, err := os.Stat(h.dir); err != nil || !info.IsDir() {
		writeErr(w, http.StatusNotFound, "前端尚未构建，请运行 npm run build")
		return
	}
	clean := filepath.Clean("/" + r.URL.Path)
	target := filepath.Join(h.dir, filepath.FromSlash(clean))
	if !strings.HasPrefix(target, h.dir) {
		writeErr(w, http.StatusBadRequest, "非法路径")
		return
	}
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		h.fs.ServeHTTP(w, r)
		return
	}
	index := filepath.Join(h.dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		writeErr(w, http.StatusNotFound, "未找到 index.html")
		return
	}
	http.ServeFile(w, r, index)
}
