<script setup lang="ts">
import { vModal } from "./modal";
import { onMounted, ref } from "vue";
import { X, Settings } from "lucide-vue-next";
const emit = defineEmits(["close"]);
const mode = ref("keep"),
  busy = ref(true),
  error = ref(""),
  saved = ref(false);
async function request(method = "GET") {
  busy.value = true;
  error.value = "";
  try {
    const r = await fetch("/api/download-settings", {
      method,
      headers: { "Content-Type": "application/json" },
      ...(method === "POST"
        ? { body: JSON.stringify({ download_mode: mode.value }) }
        : {}),
    });
    if (r.status === 401) {
      location.assign("/login");
      return;
    }
    if (!r.ok) throw Error("设置读取或保存失败，请稍后重试");
    mode.value = (await r.json()).download_mode;
    saved.value = method === "POST";
  } catch (e: any) {
    error.value = e.message;
  } finally {
    busy.value = false;
  }
}
function save() {
  if (
    mode.value === "delete" &&
    !confirm(
      "以后新加入的任务将在本地完整下载并核验后删除网盘源项。确认使用此策略？",
    )
  )
    return;
  request("POST");
}
onMounted(() => request());
</script>
<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section
      class="detail-drawer"
      v-modal="() => emit('close')"
      role="dialog"
      aria-modal="true"
      aria-labelledby="settings-title"
    >
      <header>
        <h2 id="settings-title"><Settings :size="18" /> 下载设置</h2>
        <button
          class="icon-button"
          aria-label="关闭设置"
          @click="emit('close')"
        >
          <X />
        </button>
      </header>
      <h3>新任务完成后</h3>
      <p>设置只影响以后加入的根任务，已有任务保留创建时的策略。</p>
      <form class="settings-form" @submit.prevent="save">
        <label
          ><input
            type="radio"
            v-model="mode"
            value="keep"
            :disabled="busy"
          />保留网盘文件<small
            >本地下载完整并核验后，在源项名称后标记「.可删除」。</small
          ></label
        ><label
          ><input
            type="radio"
            v-model="mode"
            value="delete"
            :disabled="busy"
          />删除网盘源项<small
            >仅在本地完整下载并核验成功后执行删除。</small
          ></label
        >
        <p v-if="error" class="notice danger" role="alert">{{ error }}</p>
        <p v-if="saved" role="status">设置已保存，将用于新任务。</p>
        <button class="button primary" :disabled="busy">
          {{ busy ? "正在处理…" : "保存设置" }}
        </button>
      </form>
      <h4>下载路径</h4>
      <p>监控目录和本地保存路径可在网盘管理的下载设置中查看和修改。</p>
      <a class="text-link" href="/cloud?ui_tab=settings">打开路径设置</a>
    </section>
  </div>
</template>
