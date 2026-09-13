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
    *) echo "" ;;
  esac
}

failed=0
for relative_path in \
  sdk/Luckfox_Aura_SDK_260521.tar.gz \
  firmware/standard/Luckfox_Aura_Buildroot_eMMC_260606.zip \
  firmware/standard/Luckfox_Aura_Buildroot_MicroSD_260606.zip \
  firmware/standard/Luckfox_Aura_Debian13_eMMC_260606.zip \
  firmware/standard/Luckfox_Aura_Debian13_MicroSD_260606.zip \
  firmware/displays/Luckfox-Aura-Debian13-eMMC-13.3inch-DSI-LCD-260317.zip \
  firmware/displays/Luckfox-Aura-Debian13-eMMC-5-DSI-TOUCH-A-260312.zip \
  firmware/displays/Luckfox-Aura-Debian13-eMMC-7-DSI-TOUCH-A-260119.zip
do
  path="$REF_DIR/$relative_path"
  expected="$(expected_size "$relative_path")"
  if [[ ! -f "$path" ]]; then
    echo "MISSING $relative_path (expected $expected bytes)"
    failed=1
    continue
  fi
  actual="$(stat -f '%z' "$path")"
  if [[ "$actual" != "$expected" ]]; then
    echo "SIZE_MISMATCH $relative_path actual=$actual expected=$expected"
    failed=1
  else
    echo "OK $relative_path $actual bytes"
  fi
done

for path in "$REF_DIR"/firmware/standard/*.zip "$REF_DIR"/firmware/displays/*.zip "$REF_DIR"/tools/*.zip; do
  [[ -f "$path" ]] || continue
  if ! file "$path" | grep -Eq 'Zip archive data|Java archive data'; then
    echo "TYPE_MISMATCH $path"
    failed=1
  fi
done

if [[ "$failed" -ne 0 ]]; then
  echo "Resource verification failed. Re-run fetch-official-resources.sh to resume downloads." >&2
  exit 1
fi

echo "All expected downloaded resource sizes and archive types are valid."
