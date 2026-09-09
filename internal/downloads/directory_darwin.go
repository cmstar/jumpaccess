//go:build darwin && cgo

package downloads

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>
static char* jumpAccessDownloads(void) {
 @autoreleasepool {
  NSArray* paths = NSSearchPathForDirectoriesInDomains(NSDownloadsDirectory, NSUserDomainMask, YES);
  if ([paths count] == 0) return NULL;
  return strdup([[paths objectAtIndex:0] fileSystemRepresentation]);
 }
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func Directory() (string, error) {
	path := C.jumpAccessDownloads()
	if path == nil {
		return "", errors.New("system download directory is unavailable")
	}
	defer C.free(unsafe.Pointer(path))
	return C.GoString(path), nil
}
