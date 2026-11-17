package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type _ICoreWebView2NavigationStartingEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	GetIsUserInitiated ComProc
	GetIsRedirected    ComProc
	GetRequestHeaders  ComProc
	GetNavigationId    ComProc
	GetCancel          ComProc
	PutCancel          ComProc
}

type ICoreWebView2NavigationStartingEventArgs struct {
	vtbl *_ICoreWebView2NavigationStartingEventArgsVtbl
}

func (i *ICoreWebView2NavigationStartingEventArgs) AddRef() uintptr {
	r, _, _ := i.vtbl.AddRef.Call()
	return r
}

// GetUri returns the URI the web view is about to navigate to. The caller
// owns the returned string (CoTaskMemFree is handled here).
func (i *ICoreWebView2NavigationStartingEventArgs) GetUri() (string, error) {
	var err error
	var _uri *uint16
	_, _, err = i.vtbl.GetUri.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&_uri)),
	)
	if err != windows.ERROR_SUCCESS {
		return "", err
	}
	uri := windows.UTF16PtrToString(_uri)
	windows.CoTaskMemFree(unsafe.Pointer(_uri))
	return uri, nil
}

// PutCancel cancels the navigation when cancel is true.
func (i *ICoreWebView2NavigationStartingEventArgs) PutCancel(cancel bool) error {
	var err error
	_, _, err = i.vtbl.PutCancel.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(cancel)),
	)
	if err != windows.ERROR_SUCCESS {
		return err
	}
	return nil
}
