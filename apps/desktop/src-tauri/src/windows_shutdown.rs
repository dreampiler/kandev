// This is the same owned-child stop event registered by the native launcher.
// The sender requests normal teardown; the caller still waits for child exit.
use std::ffi::c_void;

#[link(name = "kernel32")]
extern "system" {
    fn OpenEventW(access: u32, inherit: i32, name: *const u16) -> *mut c_void;
    fn SetEvent(event: *mut c_void) -> i32;
    fn CloseHandle(handle: *mut c_void) -> i32;
}

pub(crate) fn request(pid: u32) -> bool {
    if pid == 0 {
        return false;
    }
    let name: Vec<u16> = format!("Local\\KandevShutdown-{pid}")
        .encode_utf16()
        .chain(std::iter::once(0))
        .collect();
    unsafe {
        let event = OpenEventW(0x0002, 0, name.as_ptr()); // EVENT_MODIFY_STATE
        if event.is_null() {
            return false;
        }
        let sent = SetEvent(event) != 0;
        CloseHandle(event);
        sent
    }
}
