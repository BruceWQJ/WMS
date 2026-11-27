export async function request(path, options = {}) {
  const response = await fetch('/api' + path, {
    headers: { 'Content-Type': 'application/json' },
    ...options
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    throw new Error(body.error || response.statusText || '请求失败')
  }
  return response.json()
}

export const api = {
  health: () => request('/health'),
  getWarehouse: () => request('/warehouse'),
  updateWarehouse: (data) => request('/warehouse', { method: 'PUT', body: JSON.stringify(data) }),
  listLocations: () => request('/locations'),
  saveLocation: (data) => request('/locations', { method: 'POST', body: JSON.stringify(data) }),
  updateLocation: (id, data) => request('/locations/' + id, { method: 'PUT', body: JSON.stringify(data) }),
  deleteLocation: (id) => request('/locations/' + id, { method: 'DELETE' }),
  listInventory: () => request('/inventory'),
  setInventory: (data) => request('/inventory', { method: 'POST', body: JSON.stringify(data) }),
  seed: () => request('/seed', { method: 'POST' }),
  createOrder: (items) => request('/orders', { method: 'POST', body: JSON.stringify({ items }) }),
  listOrders: () => request('/orders'),
  getOrder: (id) => request('/orders/' + id)
}
