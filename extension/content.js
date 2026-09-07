/* ============================================================
   Clipboard Vault — Content Script
   Auto-fills and captures credentials on web pages
   ============================================================ */

(() => {
  // Listen for messages from popup
  chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    if (message.type === "AUTOFILL") {
      const success = performAutofill(message.username, message.password);
      sendResponse({ success });
      return true;
    }
  });

  function performAutofill(username, password) {
    const passwordInputs = Array.from(document.querySelectorAll('input[type="password"]'));
    if (passwordInputs.length === 0) return false;

    // Fill password into first visible password field
    const targetPassword = passwordInputs.find(isFieldVisible) || passwordInputs[0];
    if (targetPassword) {
      setInputValue(targetPassword, password);
    }

    // Try finding username/email field
    if (username) {
      const form = targetPassword ? targetPassword.closest("form") : null;
      const scope = form || document;

      // Selectors in order of priority
      const usernameInput =
        scope.querySelector('input[autocomplete="username"]') ||
        scope.querySelector('input[autocomplete="email"]') ||
        scope.querySelector('input[type="email"]') ||
        scope.querySelector('input[name*="user" i], input[name*="login" i], input[name*="email" i]') ||
        scope.querySelector('input[type="text"]');

      if (usernameInput && usernameInput !== targetPassword && isFieldVisible(usernameInput)) {
        setInputValue(usernameInput, username);
      }
    }

    return true;
  }

  function setInputValue(input, val) {
    input.focus();
    input.value = val;

    // Dispatch synthetic input and change events for reactive frontends (React, Vue, etc.)
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

  // Observe form submissions to capture credentials for saving
  document.addEventListener("submit", (e) => {
    const form = e.target;
    if (!form || !(form instanceof HTMLFormElement)) return;

    const passwordInput = form.querySelector('input[type="password"]');
    if (!passwordInput || !passwordInput.value) return;

    const usernameInput =
      form.querySelector('input[autocomplete="username"]') ||
      form.querySelector('input[type="email"]') ||
      form.querySelector('input[name*="user" i], input[name*="login" i], input[name*="email" i]') ||
      form.querySelector('input[type="text"]');

    const username = usernameInput ? usernameInput.value : "";
    const password = passwordInput.value;

    if (password) {
      try {
        chrome.runtime.sendMessage({
          type: "CREDENTIAL_SUBMITTED",
          url: window.location.href,
          username: username,
          password: password,
          title: document.title || window.location.hostname,
        });
      } catch {
        // Context might be invalidated on navigation
      }
    }
  }, true);
})();
