const inputEl = document.getElementById("input");
const proxiesEl = document.getElementById("proxies");
const groupsEl = document.getElementById("groups");
const convertBtn = document.getElementById("convert");
const clearBtn = document.getElementById("clear");
const errorsBox = document.getElementById("errors");
const errorList = document.getElementById("error-list");
const statusEl = document.getElementById("status");
const copyButtons = document.querySelectorAll("[data-copy]");
const yamlCopyBtn = document.getElementById("copy-yaml");
const yamlSample = document.getElementById("yaml-sample");

let statusTimer;

function showStatus(message) {
  statusEl.textContent = message;
  statusEl.classList.add("show");
  if (statusTimer) {
    clearTimeout(statusTimer);
  }
  statusTimer = setTimeout(() => {
    statusEl.classList.remove("show");
  }, 2200);
}

function renderErrors(errors) {
  errorList.innerHTML = "";
  if (!errors || errors.length === 0) {
    errorsBox.classList.add("hidden");
    return;
  }
  errorsBox.classList.remove("hidden");
  errors.forEach((err) => {
    const item = document.createElement("li");
    const value = err.value ? ` (${err.value})` : "";
    item.textContent = `#${err.index} ${err.message}${value}`;
    errorList.appendChild(item);
  });
}

function setLoading(isLoading) {
  convertBtn.disabled = isLoading;
  convertBtn.textContent = isLoading ? "转换中..." : "转换";
}

async function convert() {
  setLoading(true);
  renderErrors([]);
  try {
    const response = await fetch("/api/convert", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ input: inputEl.value })
    });

    if (!response.ok) {
      let message = "请求失败。";
      try {
        const data = await response.json();
        if (data && data.error) {
          message = data.error;
        }
      } catch (err) {
        // ignore parse errors
      }
      showStatus(message);
      setLoading(false);
      return;
    }

    const data = await response.json();
    proxiesEl.value = data.proxy_lines || "";
    groupsEl.value = data.group_lines || "";
    renderErrors(data.errors || []);
    showStatus("转换完成。");
  } catch (err) {
    showStatus("网络错误。");
  } finally {
    setLoading(false);
  }
}

function clearAll() {
  inputEl.value = "";
  proxiesEl.value = "";
  groupsEl.value = "";
  renderErrors([]);
  showStatus("已清空。");
}

async function copyText(text, label) {
  if (!text.trim()) {
    showStatus("暂无可复制内容。");
    return;
  }
  try {
    await navigator.clipboard.writeText(text);
    showStatus(`已复制${label}。`);
    return;
  } catch (err) {
    // fallback below
  }

  const temp = document.createElement("textarea");
  temp.value = text;
  document.body.appendChild(temp);
  temp.select();
  try {
    document.execCommand("copy");
    showStatus(`已复制${label}。`);
  } catch (err) {
    showStatus("复制失败。");
  } finally {
    document.body.removeChild(temp);
  }
}

convertBtn.addEventListener("click", convert);
clearBtn.addEventListener("click", clearAll);

copyButtons.forEach((button) => {
  button.addEventListener("click", () => {
    if (button.dataset.copy === "proxies") {
      copyText(proxiesEl.value, "节点");
    } else {
      copyText(groupsEl.value, "组内引用");
    }
  });
});

if (yamlCopyBtn && yamlSample) {
  yamlCopyBtn.addEventListener("click", () => {
    copyText(yamlSample.textContent || "", "示例");
  });
}
