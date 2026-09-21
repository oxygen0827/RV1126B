#!/usr/bin/env python3
"""LVGL 健身 UI 适配 Aura 640x480：路径、预览尺寸、会话页纵向坐标、显示尺寸。"""
import re

BASE = "/root/lvgl-app"

# ---------- 1) fitness_ui.c ----------
p = BASE + "/custom/fitness_ui.c"
s = open(p).read()

# 路径
s = s.replace('"/root/pose_deploy/upload/session_meta.json"', '"/root/yolov8s-pose/upload/session_meta.json"')
s = s.replace('"/root/pose_deploy/upload/status_ok.txt"', '"/root/yolov8s-pose/upload/status_ok.txt"')
s = s.replace('"/root/pose_deploy/upload/status_err.txt"', '"/root/yolov8s-pose/upload/status_err.txt"')
s = s.replace('"/root/pose_deploy/wifi_provision.sh"', '"/root/yolov8s-pose/wifi_provision.sh"')

# 预览尺寸：竖装摄像头后端写 360x640（9:16），显示区保持同比例 225x400
s = s.replace('#define PREVIEW_W 480\n#define PREVIEW_H 360\n#define PREVIEW_SRC_W 640\n#define PREVIEW_SRC_H 480',
              '#define PREVIEW_W 225\n#define PREVIEW_H 400\n#define PREVIEW_SRC_W 360\n#define PREVIEW_SRC_H 640')
s = s.replace('#define PREVIEW_W 480\n#define PREVIEW_H 270\n#define PREVIEW_SRC_W 640\n#define PREVIEW_SRC_H 360',
              '#define PREVIEW_W 225\n#define PREVIEW_H 400\n#define PREVIEW_SRC_W 360\n#define PREVIEW_SRC_H 640')

# 会话页纵向坐标（原按 720 高设计，480 高会溢出）
s = s.replace('''    title_label = make_label(root, "SQUAT SESSION", &lv_font_montserratMedium_30,
                             lv_color_hex(0x19324d), LV_ALIGN_TOP_MID, 0, 72);
    subtitle_label = make_label(root, "Camera preview + real-time correction", &lv_font_montserratMedium_16,
                                lv_color_hex(0x5d7187), LV_ALIGN_TOP_MID, 0, 126);
    preview_image = lv_img_create(root);
    lv_img_set_src(preview_image, &preview_dsc);
    lv_obj_align(preview_image, LV_ALIGN_TOP_MID, 0, 145);
    timer_label = make_label(root, "...", &lv_font_montserratMedium_42,
                             lv_color_hex(0x1677ff), LV_ALIGN_TOP_MID, 0, 190);
    lv_obj_align(timer_label, LV_ALIGN_TOP_MID, 0, 525);
    status_label = make_label(root, "CAMERA WAITING", &lv_font_montserratMedium_16,
                              lv_color_hex(0x1a9c5b), LV_ALIGN_TOP_MID, 0, 585);
    lv_obj_set_width(status_label, 660);''',
'''    title_label = make_label(root, "SQUAT SESSION", &lv_font_montserratMedium_30,
                             lv_color_hex(0x19324d), LV_ALIGN_TOP_MID, 125, 45);
    subtitle_label = make_label(root, "Camera preview + real-time correction", &lv_font_montserratMedium_16,
                                lv_color_hex(0x5d7187), LV_ALIGN_TOP_MID, 125, 92);
    preview_image = lv_img_create(root);
    lv_img_set_src(preview_image, &preview_dsc);
    lv_obj_align(preview_image, LV_ALIGN_TOP_LEFT, 18, 40);
    timer_label = make_label(root, "...", &lv_font_montserratMedium_42,
                             lv_color_hex(0x1677ff), LV_ALIGN_TOP_MID, 125, 160);
    status_label = make_label(root, "CAMERA WAITING", &lv_font_montserratMedium_16,
                              lv_color_hex(0x1a9c5b), LV_ALIGN_TOP_MID, 125, 240);
    lv_obj_set_width(status_label, 340);''')

# 已经应用过旧版横屏补丁的板端源码也能幂等升级到竖屏布局。
s = s.replace('''    title_label = make_label(root, "SQUAT SESSION", &lv_font_montserratMedium_30,
                             lv_color_hex(0x19324d), LV_ALIGN_TOP_MID, 0, 16);
    subtitle_label = make_label(root, "Camera preview + real-time correction", &lv_font_montserratMedium_16,
                                lv_color_hex(0x5d7187), LV_ALIGN_TOP_MID, 0, 54);
    preview_image = lv_img_create(root);
    lv_img_set_src(preview_image, &preview_dsc);
    lv_obj_align(preview_image, LV_ALIGN_TOP_MID, 0, 78);
    timer_label = make_label(root, "...", &lv_font_montserratMedium_42,
                             lv_color_hex(0x1677ff), LV_ALIGN_TOP_MID, 0, 355);
    status_label = make_label(root, "CAMERA WAITING", &lv_font_montserratMedium_16,
                              lv_color_hex(0x1a9c5b), LV_ALIGN_TOP_MID, 0, 420);
    lv_obj_set_width(status_label, 600);''',
'''    title_label = make_label(root, "SQUAT SESSION", &lv_font_montserratMedium_30,
                             lv_color_hex(0x19324d), LV_ALIGN_TOP_MID, 125, 45);
    subtitle_label = make_label(root, "Camera preview + real-time correction", &lv_font_montserratMedium_16,
                                lv_color_hex(0x5d7187), LV_ALIGN_TOP_MID, 125, 92);
    preview_image = lv_img_create(root);
    lv_img_set_src(preview_image, &preview_dsc);
    lv_obj_align(preview_image, LV_ALIGN_TOP_LEFT, 18, 40);
    timer_label = make_label(root, "...", &lv_font_montserratMedium_42,
                             lv_color_hex(0x1677ff), LV_ALIGN_TOP_MID, 125, 160);
    status_label = make_label(root, "CAMERA WAITING", &lv_font_montserratMedium_16,
                              lv_color_hex(0x1a9c5b), LV_ALIGN_TOP_MID, 125, 240);
    lv_obj_set_width(status_label, 340);''')
open(p, "w").write(s)

# ---------- 2) custom.h：声明面板尺寸 ----------
p = BASE + "/custom/custom.h"
s = open(p).read()
if "extern int PANEL_W" not in s:
    s = s.replace("#define WIDTH   480", "extern int PANEL_W, PANEL_H;\n#define WIDTH   480")
open(p, "w").write(s)

# ---------- 3) custom_main.c：记录面板尺寸 ----------
p = BASE + "/custom/custom_main.c"
s = open(p).read()
if "int PANEL_W" not in s:
    s = s.replace("float SCALE;", "float SCALE;\nint PANEL_W, PANEL_H;", 1)
s = s.replace('''                    else if(connector->modes[j].hdisplay == 640 && connector->modes[j].vdisplay == 480){
                        SCALE = 1.0f;
                    }''',
'''                    else if(connector->modes[j].hdisplay == 640 && connector->modes[j].vdisplay == 480){
                        SCALE = 1.0f;
                        PANEL_W = 640;
                        PANEL_H = 480;
                    }''')
open(p, "w").write(s)

# ---------- 4) main.c：显示尺寸用面板实际尺寸 ----------
p = BASE + "/src/main.c"
s = open(p).read()
old = '''    int disp_width = WIDTH * SCALE;
    int disp_height = HEIGHT * SCALE;'''
new = '''    int disp_width = (PANEL_W > 0) ? PANEL_W : WIDTH * SCALE;
    int disp_height = (PANEL_H > 0) ? PANEL_H : HEIGHT * SCALE;'''
if new not in s:
    assert old in s, "main.c disp size"
    s = s.replace(old, new, 1)
open(p, "w").write(s)

print("Aura UI adaptation applied")
