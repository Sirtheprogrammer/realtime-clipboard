#!/usr/bin/env python3
"""
Cross-platform Extension Packaging Script
Generates AMO-compliant Firefox zip/xpi and Chrome Web Store zip with strict forward-slash POSIX archive paths.
"""

import json
import os
import shutil
import sys
import zipfile

def build_packages():
    root_dir = os.path.dirname(os.path.abspath(__file__))
    ext_dir = os.path.join(root_dir, "extension")
    dist_dir = os.path.join(root_dir, "dist")
    os.makedirs(dist_dir, exist_ok=True)

    manifest_path = os.path.join(ext_dir, "manifest.json")
    with open(manifest_path, "r", encoding="utf-8") as f:
        manifest_data = json.load(f)

    version = manifest_data.get("version", "1.0.0")
    print(f"Packaging Clipboard Vault Extension v{version}...")

    # Files and extensions to exclude
    exclude_exts = {".go"}
    exclude_files = {".DS_Store", "extension_test.go", "extension.go"}

    def add_files_to_zip(zf, base_path, manifest_override=None):
        for root, dirs, files in os.walk(base_path):
            dirs.sort()
            for file in sorted(files):
                if file in exclude_files or any(file.endswith(ext) for ext in exclude_exts):
                    continue
                full_path = os.path.join(root, file)
                # Ensure POSIX forward slashes strictly for Mozilla AMO & Chrome
                arcname = os.path.relpath(full_path, base_path).replace("\\", "/")
                
                if arcname == "manifest.json" and manifest_override:
                    zf.writestr("manifest.json", json.dumps(manifest_override, indent=2), compress_type=zipfile.ZIP_DEFLATED)
                else:
                    zf.write(full_path, arcname, compress_type=zipfile.ZIP_DEFLATED)

    # 1. Package Firefox AMO Extension
    firefox_manifest = json.loads(json.dumps(manifest_data))
    if "background" in firefox_manifest and "service_worker" in firefox_manifest["background"]:
        del firefox_manifest["background"]["service_worker"]

    firefox_zip = os.path.join(dist_dir, "clipboard-vault-firefox.zip")
    firefox_xpi = os.path.join(dist_dir, "clipboard-vault-firefox.xpi")

    with zipfile.ZipFile(firefox_zip, "w", zipfile.ZIP_DEFLATED) as zf:
        add_files_to_zip(zf, ext_dir, manifest_override=firefox_manifest)
    
    shutil.copyfile(firefox_zip, firefox_xpi)
    print(f"Created: {firefox_zip}")
    print(f"Created: {firefox_xpi}")

    # Verify Firefox zip entries
    with zipfile.ZipFile(firefox_zip, "r") as zf:
        bad_entries = [name for name in zf.namelist() if "\\" in name]
        if bad_entries:
            raise RuntimeError(f"Found backslashes in Firefox archive: {bad_entries}")
        print("Firefox zip verified: 100% POSIX forward slash paths.")

    # 2. Package Chrome Web Store Extension
    chrome_manifest = json.loads(json.dumps(manifest_data))
    if "background" in chrome_manifest and "scripts" in chrome_manifest["background"]:
        del chrome_manifest["background"]["scripts"]

    chrome_zip = os.path.join(dist_dir, "clipboard-vault-chrome.zip")
    with zipfile.ZipFile(chrome_zip, "w", zipfile.ZIP_DEFLATED) as zf:
        add_files_to_zip(zf, ext_dir, manifest_override=chrome_manifest)
    
    print(f"Created: {chrome_zip}")

    # Verify Chrome zip entries
    with zipfile.ZipFile(chrome_zip, "r") as zf:
        bad_entries = [name for name in zf.namelist() if "\\" in name]
        if bad_entries:
            raise RuntimeError(f"Found backslashes in Chrome archive: {bad_entries}")
        print("Chrome zip verified: 100% POSIX forward slash paths.")

    print("\nAll extension packages built successfully in ./dist/!")

if __name__ == "__main__":
    build_packages()
