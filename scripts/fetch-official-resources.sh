#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REF_DIR="$ROOT_DIR/reference/luckfox-aura"

expected_size() {
  case "$1" in
    sdk/Luckfox_Aura_SDK_260521.tar.gz) echo 3018078605 ;;
    firmware/standard/Luckfox_Aura_Buildroot_eMMC_260606.zip) echo 238904525 ;;
    firmware/standard/Luckfox_Aura_Buildroot_MicroSD_260606.zip) echo 339477605 ;;
    firmware/standard/Luckfox_Aura_Debian13_eMMC_260606.zip) echo 766191850 ;;
    firmware/standard/Luckfox_Aura_Debian13_MicroSD_260606.zip) echo 1397164410 ;;
    firmware/displays/Luckfox-Aura-Debian13-eMMC-13.3inch-DSI-LCD-260317.zip) echo 1388686425 ;;
    firmware/displays/Luckfox-Aura-Debian13-eMMC-5-DSI-TOUCH-A-260312.zip) echo 1388688047 ;;
    firmware/displays/Luckfox-Aura-Debian13-eMMC-7-DSI-TOUCH-A-260119.zip) echo 1311473367 ;;
    firmware/displays/Luckfox-Aura-10.1-DSI-TOUCH-B-boot.img) echo 9695232 ;;
    firmware/displays/luckfox-config-arm64) echo 4456632 ;;
    tools/adb_fastboot.zip) echo 23800752 ;;
    tools/balenaEtcher-Portable-1.7.7.zip) echo 129100018 ;;
    tools/DriverAssitant_v5.13.zip) echo 9775583 ;;
    tools/MobaXterm_Portable_v22.0.zip) echo 27617655 ;;
    tools/RKDevTool_Release_v3.31.zip) echo 3323369 ;;
    tools/SDCardFormatter.zip) echo 6424321 ;;
    tools/SocToolKit_V2.2.zip) echo 41355660 ;;
    tools/upgrade_tool_v2.17_linux.zip) echo 1423018 ;;
    tools/upgrade_tool_v2.25_for_mac.zip) echo 559147 ;;
    tools/upgrade_tool_v2.44_for_mac.zip) echo 571379 ;;
    tools/yuvplayer-2.5.zip) echo 926323 ;;
    *) echo "" ;;
  esac
}

mkdir -p \
  "$REF_DIR/firmware/standard" \
  "$REF_DIR/firmware/displays" \
  "$REF_DIR/sdk" \
  "$REF_DIR/tools" \
  "$REF_DIR/wiki"

download() {
  local url="$1"
  local output="$2"

  local relative_output="${output#$REF_DIR/}"
  local expected
  expected="$(expected_size "$relative_output")"
  if [[ -s "$output" ]]; then
    local actual
    actual="$(stat -f '%z' "$output")"
    if [[ -z "$expected" || "$actual" == "$expected" ]]; then
      echo "Already present: ${output#$ROOT_DIR/}"
      return
    fi
    echo "Resuming incomplete file: ${output#$ROOT_DIR/} ($actual/$expected bytes)"
  fi

  echo "Downloading: ${output#$ROOT_DIR/}"
  curl --fail --location --show-error --progress-bar \
    --retry 5 --retry-delay 2 --continue-at - \
    --output "$output" "$url"
}

download_drive() {
  local file_id="$1"
  local output="$2"
  download "https://drive.usercontent.google.com/download?id=${file_id}&export=download&confirm=t" "$output"
}

# Current SDK package. The older 251224 package remains listed upstream as a backup.
download_drive "1Oc0v1YyBIvL6IpQJ_RAdBVjxK0gByA2b" \
  "$REF_DIR/sdk/Luckfox_Aura_SDK_260521.tar.gz"

# Standard Buildroot and Debian 13 images for eMMC and MicroSD variants.
download_drive "1GJ1QLeWLedw_4oJthfTzBYGH6Djouwp2" \
  "$REF_DIR/firmware/standard/Luckfox_Aura_Buildroot_eMMC_260606.zip"
download_drive "1gPHcMNJyVkzNLTJ7WUY-dc79AjzFcMm3" \
  "$REF_DIR/firmware/standard/Luckfox_Aura_Buildroot_MicroSD_260606.zip"
download_drive "1bmGTj7a8mZelMOPfOHfDAmRaxChS6q_1" \
  "$REF_DIR/firmware/standard/Luckfox_Aura_Debian13_eMMC_260606.zip"
download_drive "1x4MPURlHa8MIFAGD92Koa2AQ6PphKJ4a" \
  "$REF_DIR/firmware/standard/Luckfox_Aura_Debian13_MicroSD_260606.zip"

# Official display-specific Debian images and the 10.1-inch overlay files.
download_drive "1VEG06RblvqJylauh9RbUNRbq0yEtv2sj" \
  "$REF_DIR/firmware/displays/Luckfox-Aura-Debian13-eMMC-13.3inch-DSI-LCD-260317.zip"
download_drive "1KnjZ0763GsPe3YsJOkI9tToCbnYsNrGe" \
  "$REF_DIR/firmware/displays/Luckfox-Aura-Debian13-eMMC-5-DSI-TOUCH-A-260312.zip"
download_drive "15wUP4cbijOEfMpuPQ-gNAteaczszUuae" \
  "$REF_DIR/firmware/displays/Luckfox-Aura-Debian13-eMMC-7-DSI-TOUCH-A-260119.zip"
download_drive "1zSQru1ik94H4mf5z09TcfK7NIWT8Yige" \
  "$REF_DIR/firmware/displays/Luckfox-Aura-10.1-DSI-TOUCH-B-boot.img"
download_drive "1MIPVCNb1wJ3Xj3TUiwvimCKl8I6b9OV8" \
  "$REF_DIR/firmware/displays/luckfox-config-arm64"

# Official GitHub release tools. RKDevTool is published only in the Drive set.
RELEASE_BASE="https://github.com/LuckfoxTECH/luckfox-aura-docs/releases/download/v0.0.1"
for archive in \
  adb_fastboot.zip \
  balenaEtcher-Portable-1.7.7.zip \
  DriverAssitant_v5.13.zip \
  MobaXterm_Portable_v22.0.zip \
  SDCardFormatter.zip \
  SocToolKit_V2.2.zip \
  yuvplayer-2.5.zip
do
  download "$RELEASE_BASE/$archive" "$REF_DIR/tools/$archive"
done
download_drive "1BL67hk1HQMV_prX4f33duPNmxvbhP2ct" \
  "$REF_DIR/tools/RKDevTool_Release_v3.31.zip"

# Command-line upgrade tools linked directly from the official flashing guide.
download "https://wiki.luckfox.com/assets/files/upgrade_tool_v2.17-bfd48dcdba9fd8013872ca2abff19a8d.zip" \
  "$REF_DIR/tools/upgrade_tool_v2.17_linux.zip"
download "https://wiki.luckfox.com/assets/files/upgrade_tool_v2.25_for_mac-be4af178e48d6a55755c118d1d3c2f7e.zip" \
  "$REF_DIR/tools/upgrade_tool_v2.25_for_mac.zip"
download "https://wiki.luckfox.com/assets/files/upgrade_tool_v2.44_for_mac-d34c9648a1c9bd0e965d598dc3183b67.zip" \
  "$REF_DIR/tools/upgrade_tool_v2.44_for_mac.zip"

# Self-contained HTML responses preserve the current textual Wiki instructions.
wiki_pages=(
  index Introduction Getting-Started Login Pinout
  Aura-pinout/GPIO Aura-pinout/IIC Aura-pinout/POE Aura-pinout/PWM
  Aura-pinout/RTC Aura-pinout/SPI Aura-pinout/UART Aura-pinout/USB DSI
  SDK SDK-Image-Compilation Buildroot-Configuration Kernel-Configuration
  Buildroot Debian13 Debian13/Audio Debian13/CSI Debian13/WIFI
  RKNN RKNN-Toolkit2 RKNN-Toolkit-Lite2 RKNN-Model-Zoo Downloads
)

for page in "${wiki_pages[@]}"; do
  if [[ "$page" == "index" ]]; then
    url="https://wiki.luckfox.com/zh/Luckfox-Aura/"
    output="$REF_DIR/wiki/index.html"
  else
    url="https://wiki.luckfox.com/zh/Luckfox-Aura/$page"
    output="$REF_DIR/wiki/${page//\//-}.html"
  fi
  download "$url" "$output"
done

echo "Official Luckfox Aura resources are present under ${REF_DIR#$ROOT_DIR/}."
