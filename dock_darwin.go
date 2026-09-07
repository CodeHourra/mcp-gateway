package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

static void gatewaySetDockVisible(int visible) {
    [NSApp setActivationPolicy:visible ? NSApplicationActivationPolicyRegular : NSApplicationActivationPolicyAccessory];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

func setDockVisible(visible bool) {
	policy := C.int(0)
	if visible {
		policy = 1
	}
	application.InvokeSync(func() { C.gatewaySetDockVisible(policy) })
}
