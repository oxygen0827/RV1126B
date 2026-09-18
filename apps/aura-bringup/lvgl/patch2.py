p = '/root/lvgl-app/CMakeLists.txt'
s = open(p).read()
old = 'target_compile_options(lvgl PRIVATE -include ${CMAKE_CURRENT_SOURCE_DIR}/custom/fitness_tick.h)'
new = '''target_compile_options(lvgl PRIVATE -include ${CMAKE_CURRENT_SOURCE_DIR}/custom/fitness_tick.h)
# 板端原生构建：lv_conf.h 在 lib/ 下，lvgl 子目录需要显式包含路径
target_include_directories(lvgl PUBLIC ${CMAKE_SOURCE_DIR}/lib)'''
assert old in s
s = s.replace(old, new, 1)
open(p, 'w').write(s)
print('patched lvgl include path')
