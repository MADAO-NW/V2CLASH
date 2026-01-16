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
const guideSection = document.getElementById("guide");

// 保存原始使用指导内容，用于清空时恢复
const originalYamlContent = yamlSample ? yamlSample.textContent : "";

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
    updateGuide(data.proxy_lines, data.group_lines);
    showStatus("转换完成。");
  } catch (err) {
    showStatus("网络错误。");
  } finally {
    setLoading(false);
  }
}

function updateGuide(proxyLines, groupLines) {
  if (!proxyLines || !groupLines) {
    return;
  }

  // 为 proxy 行添加缩进（2个空格）
  const indentedProxies = proxyLines
    .split('\n')
    .map(line => '  ' + line)
    .join('\n');

  // 为 group 行添加缩进（6个空格，对齐 proxies 列表）
  const indentedGroups = groupLines
    .split('\n')
    .map(line => '      ' + line)
    .join('\n');

  const yamlContent = `proxies:
${indentedProxies}

proxy-groups:
  - name: "GLOBAL"
    type: select
    proxies:
${indentedGroups}
      - "DIRECT"
      - "REJECT"

rules:
  - DOMAIN-SUFFIX,google.com,GLOBAL
  - DOMAIN-SUFFIX,facebook.com,GLOBAL
  - DOMAIN-SUFFIX,youtube.com,GLOBAL
  - DOMAIN-SUFFIX,netflix.com,GLOBAL
  - GEOIP,CN,DIRECT
  - MATCH,GLOBAL`;

  yamlSample.textContent = yamlContent;
  // 更新按钮文字为"复制结果"
  if (yamlCopyBtn) {
    yamlCopyBtn.textContent = "复制结果";
  }
}

function clearAll() {
  inputEl.value = "";
  proxiesEl.value = "";
  groupsEl.value = "";
  renderErrors([]);
  // 恢复原始使用指导内容
  yamlSample.textContent = originalYamlContent;
  // 恢复按钮文字为"复制示例"
  if (yamlCopyBtn) {
    yamlCopyBtn.textContent = "复制示例";
  }
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
