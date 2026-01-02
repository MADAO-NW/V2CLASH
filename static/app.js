const inputEl = document.getElementById("input");
const proxiesEl = document.getElementById("proxies");
const groupsEl = document.getElementById("groups");
const convertBtn = document.getElementById("convert");
const clearBtn = document.getElementById("clear");
const errorsBox = document.getElementById("errors");
const errorList = document.getElementById("error-list");
const statusEl = document.getElementById("status");
const copyButtons = document.querySelectorAll("[data-copy]");

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
  convertBtn.textContent = isLoading ? "Converting..." : "Convert";
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
      let message = "Request failed.";
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
    showStatus("Conversion done.");
  } catch (err) {
    showStatus("Network error.");
  } finally {
    setLoading(false);
  }
}

function clearAll() {
  inputEl.value = "";
  proxiesEl.value = "";
  groupsEl.value = "";
  renderErrors([]);
  showStatus("Cleared.");
}

async function copyText(text, label) {
  if (!text.trim()) {
    showStatus("Nothing to copy.");
    return;
  }
  try {
    await navigator.clipboard.writeText(text);
    showStatus(`Copied ${label}.`);
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
    showStatus(`Copied ${label}.`);
  } catch (err) {
    showStatus("Copy failed.");
  } finally {
    document.body.removeChild(temp);
  }
}

convertBtn.addEventListener("click", convert);
clearBtn.addEventListener("click", clearAll);

copyButtons.forEach((button) => {
  button.addEventListener("click", () => {
    if (button.dataset.copy === "proxies") {
      copyText(proxiesEl.value, "proxies");
    } else {
      copyText(groupsEl.value, "groups");
    }
  });
});
