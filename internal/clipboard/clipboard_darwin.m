#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

long clipboardChangeCount() {
    return [[NSPasteboard generalPasteboard] changeCount];
}

// clipboardReadImage reads image data as PNG from the pasteboard.
// macOS converts TIFF→PNG in-process (no subprocess).
// Caller must free() the returned pointer.
void* clipboardReadImage(int* outLen) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    NSData* data = [pb dataForType:NSPasteboardTypePNG];
    if (data == nil) {
        *outLen = 0;
        return NULL;
    }
    *outLen = (int)[data length];
    void* buf = malloc(*outLen);
    memcpy(buf, [data bytes], *outLen);
    return buf;
}

// clipboardReadText reads string data from the pasteboard.
// Caller must free() the returned pointer.
void* clipboardReadText(int* outLen) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    NSString* str = [pb stringForType:NSPasteboardTypeString];
    if (str == nil) {
        *outLen = 0;
        return NULL;
    }
    const char* utf8 = [str UTF8String];
    *outLen = (int)strlen(utf8);
    void* buf = malloc(*outLen);
    memcpy(buf, utf8, *outLen);
    return buf;
}

// clipboardWriteImage writes PNG image data to the pasteboard.
void clipboardWriteImage(const void* data, int len) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    NSData* nsData = [NSData dataWithBytes:data length:len];
    [pb setData:nsData forType:NSPasteboardTypePNG];
}

// clipboardWriteImageAndText writes PNG image + text string to the pasteboard.
void clipboardWriteImageAndText(const void* imgData, int imgLen, const char* text) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    NSData* nsData = [NSData dataWithBytes:imgData length:imgLen];
    [pb setData:nsData forType:NSPasteboardTypePNG];
    [pb setString:[NSString stringWithUTF8String:text] forType:NSPasteboardTypeString];
}

// clipboardWriteText writes a string to the pasteboard.
void clipboardWriteText(const char* text, int len) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    NSString* str = [[NSString alloc] initWithBytes:text length:len encoding:NSUTF8StringEncoding];
    [pb setString:str forType:NSPasteboardTypeString];
}
