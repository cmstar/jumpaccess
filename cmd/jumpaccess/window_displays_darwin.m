//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include <stdint.h>

static NSScreen *JumpAccessCurrentScreen(void) {
    NSScreen *active = [[NSApp keyWindow] screen];
    if (active == nil) {
        active = [[NSApp mainWindow] screen];
    }
    if (active == nil) {
        for (NSWindow *window in [NSApp windows]) {
            if ([window screen] != nil) {
                active = [window screen];
                break;
            }
        }
    }
    return active != nil ? active : [NSScreen mainScreen];
}

int JumpAccessDisplayCount(void) {
    __block int result = 0;
    void (^readCount)(void) = ^{
        result = (int)[[NSScreen screens] count];
    };
    if ([NSThread isMainThread]) {
        readCount();
    } else {
        dispatch_sync(dispatch_get_main_queue(), readCount);
    }
    return result;
}

int JumpAccessDisplayAt(int index, uint32_t *displayID, int *x, int *y, int *width, int *height, int *primary, int *current) {
    __block int found = 0;
    void (^readDisplay)(void) = ^{
        NSArray<NSScreen *> *screens = [NSScreen screens];
        if (index < 0 || index >= (int)[screens count]) {
            return;
        }
        NSScreen *screen = [screens objectAtIndex:index];
        NSScreen *active = JumpAccessCurrentScreen();
        NSNumber *number = [[screen deviceDescription] objectForKey:@"NSScreenNumber"];
        NSNumber *activeNumber = [[active deviceDescription] objectForKey:@"NSScreenNumber"];
        NSRect visible = [screen visibleFrame];
        *displayID = [number unsignedIntValue];
        *x = (int)visible.origin.x;
        // 统一为从虚拟桌面顶部向下增长的坐标，便于跨屏幕计算。
        *y = -(int)(visible.origin.y + visible.size.height);
        *width = (int)visible.size.width;
        *height = (int)visible.size.height;
        *primary = index == 0;
        *current = [number isEqualToNumber:activeNumber];
        found = 1;
    };
    if ([NSThread isMainThread]) {
        readDisplay();
    } else {
        dispatch_sync(dispatch_get_main_queue(), readDisplay);
    }
    return found;
}
