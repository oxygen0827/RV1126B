// Build/run on the board: cc check_media_abi.c -o /tmp/check-media-abi
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <linux/videodev2.h>
#include <rga/drmrga.h>
#include <rga/rga.h>
int main(void) {
    _Static_assert(offsetof(rga_info_t, rotation) == 72, "RGA rotation ABI changed");
    _Static_assert(offsetof(rga_info_t, rect) == 32, "RGA rect ABI changed");
    _Static_assert(offsetof(rga_info_t, scale_mode) == 104, "RGA scale ABI changed");
    _Static_assert(HAL_TRANSFORM_ROT_180 == 3, "RGA transform changed");
    _Static_assert(RK_FORMAT_YUYV_422 == (0x1c << 8), "YUYV format changed");
    _Static_assert(sizeof(struct v4l2_format) == 208, "V4L2 format ABI changed");
    _Static_assert(sizeof(struct v4l2_buffer) == 88, "V4L2 buffer ABI changed");
    printf("media ABI OK: rga_info=%zu rotation=%zu V4L2=%zu/%zu\n",
           sizeof(rga_info_t), offsetof(rga_info_t, rotation),
           sizeof(struct v4l2_format), sizeof(struct v4l2_buffer));
    return 0;
}
