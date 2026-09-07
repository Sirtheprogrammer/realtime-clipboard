/* ============================================================
   Clipboard Vault — Content Script
   Automatic credential detection, autofill, and automatic saving
   ============================================================ */

(() => {
  // Prevent duplicate injection
  if (window.__clipboardVaultLoaded) return;
  window.__clipboardVaultLoaded = true;

  let pageSecrets = [];
  let autofilled = false;
  let activeDropdown = null;

  // ───────────────────────── Initialization & Auto-Detection ─────────────────────────

  init();

  function init() {
    // 1. Initial query for stored credentials on page load
    fetchMatchingCredentials();

    // 2. Watch for dynamic form additions (SPAs, login modals, React/Vue dialogs)
    const observer = new MutationObserver(debounce(() => {
      if (document.querySelector('input[type="password"]')) {
        if (!autofilled && pageSecrets.length > 0) {
          applyAutofill();
        } else if (pageSecrets.length === 0) {
          fetchMatchingCredentials();
        }
      }
    }, 250));

    observer.observe(document.documentElement, {
      childList: true,
      subtree: true,
    });

    // 3. Form submit & credential capture listeners
    attachFormCaptureListeners();

    // 4. Message listener from popup (for manual trigger)
    chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
      if (message.type === "AUTOFILL") {
        const success = performAutofill(message.username, message.password);
        sendResponse({ success });
        return true;
      }
    });
  }

  // ───────────────────────── Query & Autofill Logic ─────────────────────────

  function fetchMatchingCredentials() {
    try {
      chrome.runtime.sendMessage(
        {
          type: "QUERY_CREDENTIALS",
          hostname: window.location.hostname,
        },
        (res) => {
          if (chrome.runtime.lastError || !res) return;
          if (res.secrets && res.secrets.length > 0) {
            pageSecrets = res.secrets;
            applyAutofill();
          }
        }
      );
    } catch {
      // Extension context may be reloaded
    }
  }

  function applyAutofill() {
    const passwordInputs = Array.from(document.querySelectorAll('input[type="password"]')).filter(isFieldVisible);
    if (passwordInputs.length === 0) return;

    if (pageSecrets.length === 1) {
      // Single credential: Automatic instant fill
      const sec = pageSecrets[0];
      const filled = performAutofill(sec.username, sec.value);
      if (filled) {
        autofilled = true;
      }
    } else if (pageSecrets.length > 1) {
      // Multiple credentials: Attach quick selector dropdown to username or password input
      attachCredentialPicker(pageSecrets);
    }
  }

  function performAutofill(username, password) {
    const passwordInputs = Array.from(document.querySelectorAll('input[type="password"]')).filter(isFieldVisible);
    if (passwordInputs.length === 0) return false;

    const targetPassword = passwordInputs[0];
    if (password) {
      setInputValue(targetPassword, password);
    }

    if (username) {
      const usernameInput = findUsernameInput(targetPassword);
      if (usernameInput) {
        setInputValue(usernameInput, username);
      }
    }

    return true;
  }

  function findUsernameInput(targetPassword) {
    const form = targetPassword ? targetPassword.closest("form") : null;
    const scope = form || document;

    const candidate =
      scope.querySelector('input[autocomplete="username"]') ||
      scope.querySelector('input[autocomplete="email"]') ||
      scope.querySelector('input[type="email"]') ||
      scope.querySelector('input[name*="user" i], input[name*="login" i], input[name*="email" i], input[id*="user" i], input[id*="login" i], input[id*="email" i]') ||
      scope.querySelector('input[type="text"]');

    if (candidate && candidate !== targetPassword && isFieldVisible(candidate)) {
      return candidate;
    }
    return null;
  }

  function setInputValue(input, val) {
    if (!input || input.value === val) return;

    input.focus();
    input.value = val;

    // Dispatch synthetic events for React, Vue, Angular, Svelte, and native listeners
    input.dispatchEvent(new Event("input", { bubbles: true, cancelable: true }));
    input.dispatchEvent(new Event("change", { bubbles: true, cancelable: true }));
    input.blur();
  }

  function isFieldVisible(el) {
    if (!el) return false;
    const style = window.getComputedStyle(el);
    return (
      style.display !== "none" &&
      style.visibility !== "hidden" &&
      style.opacity !== "0" &&
      el.offsetWidth > 0 &&
      el.offsetHeight > 0
    );
  }

  // ───────────────────────── Multi-Account In-Field Picker ─────────────────────────

  function attachCredentialPicker(secrets) {
    const passwordInput = document.querySelector('input[type="password"]');
    if (!passwordInput) return;
    const usernameInput = findUsernameInput(passwordInput) || passwordInput;

    const showPicker = () => {
      if (activeDropdown) activeDropdown.remove();

      const rect = usernameInput.getBoundingClientRect();
      const dropdown = document.createElement("div");
      dropdown.id = "clipboard-vault-picker";
      dropdown.style.cssText = `
        position: fixed;
        top: ${rect.bottom + window.scrollY + 4}px;
        left: ${rect.left + window.scrollX}px;
        width: ${Math.max(rect.width, 240)}px;
        background: #121418;
        border: 1px solid #2e3340;
        border-radius: 8px;
        box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
        z-index: 2147483647;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
        font-size: 12.5px;
        color: #f4f5f8;
        padding: 6px;
        box-sizing: border-box;
      `;

      const header = document.createElement("div");
      header.style.cssText = `
        font-size: 10px;
        font-weight: 700;
        letter-spacing: 0.05em;
        text-transform: uppercase;
        color: #8b92a5;
        padding: 4px 8px 6px;
        border-bottom: 1px solid #232733;
        margin-bottom: 4px;
        display: flex;
        justify-content: space-between;
      `;
      header.innerHTML = `<span>Clipboard Vault</span><span>${secrets.length} Accounts</span>`;
      dropdown.appendChild(header);

      secrets.forEach((sec) => {
        const item = document.createElement("div");
        item.style.cssText = `
          padding: 8px 10px;
          border-radius: 6px;
          cursor: pointer;
          display: flex;
          flex-direction: column;
          gap: 2px;
          transition: background 0.1s;
        `;
        item.innerHTML = `
          <b style="font-size: 12px; color: #ffffff;">${escapeHtml(sec.username || sec.title)}</b>
          <span style="font-size: 10.5px; color: #8b92a5;">${escapeHtml(sec.title || "Saved Password")}</span>
        `;
        item.addEventListener("mouseenter", () => {
          item.style.background = "#232733";
        });
        item.addEventListener("mouseleave", () => {
          item.style.background = "transparent";
        });
        item.addEventListener("mousedown", (e) => {
          e.preventDefault();
          performAutofill(sec.username, sec.value);
          dropdown.remove();
          activeDropdown = null;
        });
        dropdown.appendChild(item);
      });

      document.body.appendChild(dropdown);
      activeDropdown = dropdown;
    };

    usernameInput.addEventListener("focus", showPicker);
    document.addEventListener("click", (e) => {
      if (activeDropdown && !activeDropdown.contains(e.target) && e.target !== usernameInput) {
        activeDropdown.remove();
        activeDropdown = null;
      }
    });
  }

  // ───────────────────────── Automatic Save & Update ─────────────────────────

  function attachFormCaptureListeners() {
    // 1. Submit event listener
    document.addEventListener("submit", handleFormSubmit, true);

    // 2. Fallback: Listen for Enter key on password inputs
    document.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && e.target && e.target.matches('input[type="password"]')) {
        const form = e.target.closest("form");
        if (form) {
          handleFormSubmit({ target: form });
        } else {
          captureStandaloneInputs(e.target);
        }
      }
    }, true);

    // 3. Fallback: Listen for clicks on buttons with login/submit keywords
    document.addEventListener("click", (e) => {
      const btn = e.target.closest('button, input[type="submit"], a[role="button"]');
      if (!btn) return;

      const text = (btn.innerText || btn.value || "").toLowerCase();
      if (/log\s*in|sign\s*in|submit|continue|next|authorize/i.test(text)) {
        const form = btn.closest("form");
        if (form) {
          handleFormSubmit({ target: form });
        } else {
          const pass = document.querySelector('input[type="password"]');
          if (pass && pass.value) captureStandaloneInputs(pass);
        }
      }
    }, true);
  }

  function handleFormSubmit(e) {
    const form = e.target;
    if (!form || !(form instanceof HTMLElement)) return;

    const passwordInput = form.querySelector('input[type="password"]');
    if (!passwordInput || !passwordInput.value) return;

    const usernameInput = findUsernameInput(passwordInput);
    const username = usernameInput ? usernameInput.value : "";
    const password = passwordInput.value;

    triggerAutoSave(username, password);
  }

  function captureStandaloneInputs(passwordInput) {
    if (!passwordInput || !passwordInput.value) return;
    const usernameInput = findUsernameInput(passwordInput);
    const username = usernameInput ? usernameInput.value : "";
    const password = passwordInput.value;

    triggerAutoSave(username, password);
  }

  function triggerAutoSave(username, password) {
    if (!password) return;

    try {
      chrome.runtime.sendMessage(
        {
          type: "AUTO_SAVE_CREDENTIAL",
          url: window.location.href,
          hostname: window.location.hostname,
          username: username,
          password: password,
          title: document.title || window.location.hostname,
        },
        (res) => {
          if (chrome.runtime.lastError || !res) return;

          if (res.success) {
            if (res.status === "created") {
              showInPageNotification("🔒 Clipboard Vault: Saved password for " + (username || window.location.hostname));
            } else if (res.status === "updated") {
              showInPageNotification("🔒 Clipboard Vault: Updated password for " + (username || window.location.hostname));
            }
          }
        }
      );
    } catch {
      // Context might be invalidated on page unload
    }
  }

  // ───────────────────────── In-Page Notification ─────────────────────────

  function showInPageNotification(message) {
    const existing = document.getElementById("clipboard-vault-toast");
    if (existing) existing.remove();

    const toast = document.createElement("div");
    toast.id = "clipboard-vault-toast";
    toast.style.cssText = `
      position: fixed;
      top: 18px;
      right: 18px;
      background: #09090b;
      color: #f4f4f5;
      border: 1px solid #27272a;
      border-radius: 8px;
      padding: 10px 16px;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      font-size: 13px;
      font-weight: 500;
      box-shadow: 0 10px 25px rgba(0, 0, 0, 0.4);
      z-index: 2147483647;
      display: flex;
      align-items: center;
      gap: 10px;
      animation: cbSlideIn 0.25s cubic-bezier(0.16, 1, 0.3, 1);
      transition: opacity 0.2s, transform 0.2s;
    `;

    toast.innerHTML = `
      <span style="display:inline-block; width:8px; height:8px; border-radius:50%; background:#10b981;"></span>
      <span>${escapeHtml(message)}</span>
    `;

    document.body.appendChild(toast);

    setTimeout(() => {
      toast.style.opacity = "0";
      toast.style.transform = "translateY(-6px)";
      setTimeout(() => toast.remove(), 250);
    }, 3500);
  }

  // ───────────────────────── Helpers ─────────────────────────

  function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
      const later = () => {
        clearTimeout(timeout);
        func(...args);
      };
      clearTimeout(timeout);
      timeout = setTimeout(later, wait);
    };
  }

  function escapeHtml(str) {
    return (str || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }
})();
