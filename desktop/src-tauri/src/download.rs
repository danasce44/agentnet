//! Linux WebKit downloads blob links through its native download facility.
//! Never turn blob HTML into navigation with the app's IPC authority.
use tauri::{Manager, Url, WebviewWindow};

#[cfg(target_os = "linux")]
thread_local! {
    // WebKit objects stay on their native main thread; never in Shell's Send state.
    static ACTIVE: std::cell::RefCell<std::collections::HashMap<String, (webkit2gtk::Download, std::rc::Rc<std::cell::Cell<bool>>)>> = Default::default();
}

pub fn notice(window: &WebviewWindow, url: &str, name: &str, state: &str, progress: Option<f64>) {
    let data = serde_json::json!({"url":url,"name":name,"state":state,"progress":progress});
    let _ = window.eval(format!("window.__agentnetDownloadProgress?.({data})"));
}

#[tauri::command]
pub async fn agentnet_cancel_download(window: WebviewWindow, url: String) -> Result<(), String> {
    let actual = window.url().map_err(|_| "Download is unavailable.")?;
    let blob = Url::parse(&url).map_err(|_| "Invalid download.")?;
    {
        let shell = window.state::<crate::Shell>();
        let page = shell.page.lock().unwrap();
        if window.label() != "main" || !page.as_ref().is_some_and(|p| crate::same_origin(p, &actual) && crate::navigation::page_blob(p, &blob)) {
            return Err("Download is unavailable on this page.".into());
        }
    }
    #[cfg(target_os = "linux")]
    {
        window.with_webview(move |_| {
            use webkit2gtk::DownloadExt;
            let active = ACTIVE.with(|downloads| downloads.borrow().get(&url).cloned());
            if let Some((download, cancelled)) = active { cancelled.set(true); download.cancel(); }
        }).map_err(|_| "The download could not be cancelled.".to_string())
    }
    #[cfg(not(target_os = "linux"))]
    { Err("Use this platform's download controls.".into()) }
}

fn filename(name: &str) -> bool {
    !name.is_empty() && name.len() <= 255 && name != "." && name != ".."
        && !name.contains(['/', '\\', ':']) && !name.chars().any(|c| c.is_control())
}

#[tauri::command]
pub async fn agentnet_download_blob(window: WebviewWindow, url: String, name: String) -> Result<(), String> {
    let actual = window.url().map_err(|_| "Download is unavailable.")?;
    let blob = Url::parse(&url).map_err(|_| "Invalid download.")?;
    if !filename(&name) { return Err("Invalid download filename.".into()); }
    {
        let shell = window.state::<crate::Shell>();
        let page = shell.page.lock().unwrap();
        if window.label() != "main" || !page.as_ref().is_some_and(|p| crate::same_origin(p, &actual) && crate::navigation::page_blob(p, &blob)) {
            return Err("Download is unavailable on this page.".into());
        }
        let mut pending = shell.downloads.lock().unwrap();
        if pending.len() >= 32 { return Err("Too many downloads are starting. Try again shortly.".into()); }
        pending.insert(url.clone(), name.into());
    }
    #[cfg(target_os = "linux")]
    {
        let uri = url.clone();
        let native_window = window.clone();
        let (done, result) = std::sync::mpsc::channel();
        if window.with_webview(move |view| {
            use webkit2gtk::{DownloadExt, WebViewExt};
            if ACTIVE.with(|downloads| downloads.borrow().len() >= 32) {
                native_window.state::<crate::Shell>().downloads.lock().unwrap().remove(&uri);
                let _ = done.send(Err("Too many active downloads. Try again shortly.".to_string()));
                return;
            }
            let Some(download) = view.inner().download_uri(&uri) else {
                native_window.state::<crate::Shell>().downloads.lock().unwrap().remove(&uri);
                let _ = done.send(Err("The download could not start.".to_string()));
                return;
            };
            let cancelled = std::rc::Rc::new(std::cell::Cell::new(false));
            ACTIVE.with(|downloads| downloads.borrow_mut().insert(uri.clone(), (download.clone(), cancelled.clone())));
			// The native handle now exists, so cancellation can reach WebKit
			// even when a blob emits all received data in one batch.
			notice(&native_window, &uri, "", "saving", None);
            let failed = std::rc::Rc::new(std::cell::RefCell::new(None));
            let failure = failed.clone();
            download.connect_failed(move |_, error| {
                *failure.borrow_mut() = Some(if cancelled.get() && error.matches(webkit2gtk::DownloadError::CancelledByUser) {
                    "Download cancelled.".to_string()
                } else { "The download did not finish. Retry from its message.".to_string() });
            });
            let progress_window = native_window.clone();
            let progress_uri = uri.clone();
            let last_progress = std::cell::Cell::new(-1i32);
            download.connect_received_data(move |download, _| {
                let progress = download.estimated_progress().clamp(0.0, 1.0);
                let percent = (progress * 100.0) as i32;
                if last_progress.replace(percent) != percent {
                    notice(&progress_window, &progress_uri, "", "saving", Some(progress));
                }
            });
            download.connect_finished(move |_| {
                ACTIVE.with(|downloads| downloads.borrow_mut().remove(&uri));
                native_window.state::<crate::Shell>().downloads.lock().unwrap().remove(&uri);
                let _ = done.send(match failed.borrow_mut().take() { Some(error) => Err(error), None => Ok(()) });
            });
        }).is_err() {
            window.state::<crate::Shell>().downloads.lock().unwrap().remove(&url);
            return Err("The download could not start.".into());
        }
        // Completion is WebKit's terminal download signal, not IPC acceptance.
        tauri::async_runtime::spawn_blocking(move || result.recv().unwrap_or_else(|_| Err("The download ended before completion.".into())))
            .await.map_err(|_| "The download ended before completion.".to_string())?
    }
    #[cfg(not(target_os = "linux"))]
    { window.state::<crate::Shell>().downloads.lock().unwrap().remove(&url); Err("Use this platform's normal download link.".into()) }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn download_names_cannot_choose_a_path() {
        for n in ["agent-result.txt", "unsafe.html", "picture.svg"] { assert!(filename(n)); }
        for n in ["", ".", "..", "../secret", "/tmp/secret", "C:\\secret", "x\0y", "x\ny"] { assert!(!filename(n)); }
    }
}
