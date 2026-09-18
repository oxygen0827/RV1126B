p = '/root/lvgl-app/custom/custom_main.c'
s = open(p).read()
old = '''                if( j == 0 ){
                    // Just Support  Square screen
                    if(connector->modes[j].hdisplay == connector->modes[j].vdisplay ){
                        // 720 x 720: 1.5 ;480 x 480: 1.0
                        SCALE = (float)connector->modes[j].hdisplay / (float)HEIGHT;
                    }
                    else{
                        perror("This LCD model is not supported yet.\\n");
                        exit(EXIT_FAILURE);
                    }
                }'''
new = '''                if( j == 0 ){
                    // Aura/RV1126B: 640x480 DSI 面板（横向）；UI 基数 480x480，
                    // 先按高度 1:1 渲染在左侧，布局适配见 apps/aura-bringup README。
                    if(connector->modes[j].hdisplay == connector->modes[j].vdisplay ){
                        SCALE = (float)connector->modes[j].hdisplay / (float)HEIGHT;
                    }
                    else if(connector->modes[j].hdisplay == 640 && connector->modes[j].vdisplay == 480){
                        SCALE = 1.0f;
                    }
                    else{
                        perror("This LCD model is not supported yet.\\n");
                        exit(EXIT_FAILURE);
                    }
                }'''
assert old in s
s = s.replace(old, new, 1)
open(p, 'w').write(s)
print('patched lcd check')
