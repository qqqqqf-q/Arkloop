//go:build darwin

#import <AVFoundation/AVFoundation.h>

int arkloopMicAuthorizationStatus(void) {
    return (int)[AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
}
