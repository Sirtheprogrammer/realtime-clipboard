# Clipboard Vault — Browser Extension

Cross-browser extension (Manifest V3) for **Chrome, Firefox, Brave, Edge, and other WebExtension-compatible browsers**.

It brings the **hardware-grade AES-256-GCM encrypted Clipboard Secret Store** directly into your browser workflow:
- ⚡ **Auto-detect Active Domain**: Instantly matches and displays saved credentials for whichever website you have open.
- 🎯 **1-Click Autofill**: Automatically injects credentials into login forms without having to copy-paste.
- 📋 **1-Click Copy**: Copy username or masked password to clipboard in one tap.
- 💾 **Quick Save & Password Generator**: Store new credentials with auto-generated strong 24-character passwords.
- 🔒 **End-to-End Encryption**: Credentials are sent over HTTPS/WSS and encrypted with AES-256-GCM envelope encryption on the server before storage in PostgreSQL.

---

## Installation Guide

### 1. Google Chrome / Chromium / Brave / Microsoft Edge

1. Open your browser and navigate to the Extensions page:
   - **Chrome / Brave**: `chrome://extensions/`
   - **Edge**: `edge://extensions/`
2. Toggle on **Developer mode** (switch in the top-right corner on Chrome/Brave, or bottom-left on Edge).
3. Click the **Load unpacked** button.
4. Select the directory:
   ```
   D:\clipboard\extension
   ```
5. The **Clipboard Vault** icon will appear in your browser extension toolbar! Pin it for quick access.

---

### 2. Mozilla Firefox

1. Open Firefox and enter in the address bar:
   ```
   about:debugging#/runtime/this-firefox
   ```
2. Click **Load Temporary Add-on...**.
3. Navigate to `D:\clipboard\extension` and select the file:
   ```
   manifest.json
   ```
4. The extension will load immediately with full Manifest V3 compatibility.

---

## Configuration & Usage

1. **Start the Clipboard Server**:
   Ensure your Clipboard instance is running (by default at `http://localhost:8080` or your custom domain).
2. **Open Extension Popup**:
   - Click the gear icon (`⚙`) to configure the server URL if running on a custom host or port.
   - The status indicator turns **green** when the connection is live.
3. **Sign In**:
   - Enter your email and password registered on Clipboard.
   - Alternatively, sign in on the web app using GitHub OAuth, and manage your account.
4. **Browse Any Website**:
   - When you visit a site (e.g., `github.com`), the extension displays existing credentials or offers to save new ones.
   - Click **Autofill** on any login page to fill fields immediately!
