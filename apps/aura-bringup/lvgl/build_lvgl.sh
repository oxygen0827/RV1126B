#!/bin/sh
cd /root/lvgl-app
rm -rf build-native
GLIBC_COMPILER=/usr/bin/ cmake -S . -B build-native -G "Unix Makefiles" -DSYSTEM_UBUNTU=ON -DCMAKE_BUILD_TYPE=Release > /tmp/lvgl-cmake.log 2>&1
echo "cmake exit: $?"
tail -4 /tmp/lvgl-cmake.log
cmake --build build-native -j4 > /tmp/lvgl-build.log 2>&1
echo "build exit: $?"
tail -6 /tmp/lvgl-build.log
ls -la build-native/luckfox_lvgl_demo 2>/dev/null
