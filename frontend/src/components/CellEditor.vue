<script setup>
// CellEditor 库位编辑器：维护单个格点的类型、巷道、方向、优先级与库存，支持新建/保存/删除。
import { reactive, watch } from 'vue'
import { api } from '../api'

const props = defineProps({
  x: { type: Number, required: true }, // 格点横坐标
  y: { type: Number, required: true }, // 格点纵坐标
  location: { type: Object, default: null }, // 已有库位，为空表示新建
  skus: { type: Array, default: () => [] } // 可选 SKU 列表
})
const emit = defineEmits(['saved', 'deleted', 'close'])

// 编辑表单：存储类型、巷道、通行方向、拣料优先级、库存行
const form = reactive({
  storageType: 'FLOOR',
  aisleId: '',
  direction: 'BIDIRECTIONAL',
  pickingPriority: 10,
  inventory: []
})

const error = reactive({ value: '' })
const busy = reactive({ value: false })

// 用当前库位数据回填表单，并按类型校正默认方向
function reset() {
  const loc = props.location
  form.storageType = loc?.storageType || 'FLOOR'
  form.aisleId = loc?.aisleId || ''
  form.direction = loc?.direction || (form.storageType === 'RACK' ? 'NORTH_TO_SOUTH' : 'BIDIRECTIONAL')
  if (form.storageType === 'RACK' && !['NORTH_TO_SOUTH', 'SOUTH_TO_NORTH'].includes(form.direction)) {
    form.direction = 'NORTH_TO_SOUTH'
  }
  form.pickingPriority = loc?.pickingPriority ?? 10
  form.inventory = Object.entries(loc?.inventory || {}).map(([sku, qty]) => ({ sku, qty }))
  error.value = ''
}

watch(() => props.location, reset, { immediate: true })
watch(() => [props.x, props.y], reset)
// 切换到高架区时，方向只允许南北单向
watch(() => form.storageType, (type) => {
  if (type === 'RACK' && !['NORTH_TO_SOUTH', 'SOUTH_TO_NORTH'].includes(form.direction)) {
    form.direction = 'NORTH_TO_SOUTH'
  }
})

// 新增一行库存记录
function addRow() {
  form.inventory.push({ sku: '', qty: 10 })
}

// 删除指定库存行
function removeRow(index) {
  form.inventory.splice(index, 1)
}

// 校验库存数量后提交：已有库位走更新，否则新建
async function save() {
  busy.value = true
  error.value = ''
  const inventory = {}
  for (const row of form.inventory) {
    const sku = String(row.sku || '').trim()
    const qty = Number(row.qty)
    if (!sku) continue
    if (!Number.isInteger(qty) || qty < 0) {
      error.value = '库存数量必须为非负整数'
      busy.value = false
      return
    }
    if (qty > 0) inventory[sku] = qty
  }
  const payload = {
    x: props.x,
    y: props.y,
    storageType: form.storageType,
    aisleId: String(form.aisleId || '').trim(),
    direction: form.direction,
    pickingPriority: Number(form.pickingPriority),
    inventory
  }
  try {
    const saved = props.location?.id
      ? await api.updateLocation(props.location.id, payload)
      : await api.saveLocation(payload)
    emit('saved', saved)
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

// 删除当前已有库位
async function remove() {
  if (!props.location?.id) return
  busy.value = true
  try {
    await api.deleteLocation(props.location.id)
    emit('deleted')
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="section panel">
    <h2>库位编辑 <span class="tag">({{ x }}, {{ y }}) · {{ location?.id ? '#' + location.id : '新建' }}</span></h2>

    <datalist id="skuOptions">
      <option v-for="sku in skus" :key="sku" :value="sku" />
    </datalist>

    <div class="field-row">
      <div class="field">
        <label>存储类型</label>
        <select v-model="form.storageType">
          <option value="FLOOR">FLOOR 地堆区</option>
          <option value="RACK">RACK 高架区</option>
        </select>
      </div>
      <div class="field">
        <label>巷道编号</label>
        <input v-model="form.aisleId" placeholder="如 A04" />
      </div>
    </div>

    <div class="field-row">
      <div class="field">
        <label>通行方向</label>
        <select v-model="form.direction">
          <option v-if="form.storageType === 'FLOOR'" value="BIDIRECTIONAL">BIDIRECTIONAL 双向</option>
          <option value="NORTH_TO_SOUTH">NORTH_TO_SOUTH ↓</option>
          <option value="SOUTH_TO_NORTH">SOUTH_TO_NORTH ↑</option>
          <option v-if="form.storageType === 'FLOOR'" value="EAST_TO_WEST">EAST_TO_WEST ←</option>
          <option v-if="form.storageType === 'FLOOR'" value="WEST_TO_EAST">WEST_TO_EAST →</option>
        </select>
      </div>
      <div class="field">
        <label>拣料优先级</label>
        <input v-model.number="form.pickingPriority" type="number" />
      </div>
    </div>

    <div class="field">
      <label>库存 (SKU / 数量)</label>
      <div v-for="(row, index) in form.inventory" :key="index" class="order-line">
        <input v-model="row.sku" list="skuOptions" class="sku" placeholder="SKU_A" />
        <input v-model.number="row.qty" type="number" min="0" class="qty" />
        <button @click="removeRow(index)">×</button>
      </div>
      <button @click="addRow">+ 添加 SKU</button>
    </div>

    <p v-if="error.value" class="error">{{ error.value }}</p>

    <div class="toolbar" style="margin-top: 10px;">
      <button class="primary" :disabled="busy.value" @click="save">保存库位</button>
      <button v-if="location?.id" class="danger" :disabled="busy.value" @click="remove">删除库位</button>
      <span class="grow"></span>
      <button @click="emit('close')">关闭</button>
    </div>

    <p class="hint" style="margin-top: 8px;">
      高架区建议配置为单向通行；拣料优先级数值越大越优先扣减。
    </p>
  </div>
</template>
