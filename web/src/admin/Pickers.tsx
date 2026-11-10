import { useEffect } from 'react';
import { T } from './texts';
import { formatFileSize } from './wizard';

export interface ImageEntry {
  filename: string;
  path: string; // "/imgs/<name>"
}

export interface FileEntry {
  filename: string;
  path: string; // "/attach/<name>"
  size: number;
}

// The address of a picked image for an <img>: the name is encoded, so names
// with "#" or "?" load too.
function imageSource(image: ImageEntry): string {
  return '/imgs/' + encodeURIComponent(image.filename);
}

function PickerHeader({ title, onClose }: { title: string; onClose: () => void }) {
  return (
    <div className="modal-header">
      <h2>{title}</h2>
      <button type="button" className="close" aria-label={T.close} onClick={onClose}>
        {T.close}
      </button>
    </div>
  );
}

// Images of quizzes/imgs (#image-picker-modal).
export function ImagePicker({
  images,
  onSelect,
  onClose,
}: {
  images: ImageEntry[];
  onSelect: (path: string) => void;
  onClose: () => void;
}) {
  return (
    <div id="image-picker-modal" className="modal open">
      <div className="modal-content">
        <PickerHeader title={T.imagePickerTitle} onClose={onClose} />
        <div id="image-picker-content">
          <div id="image-grid" className="image-grid">
            {images.length === 0 ? (
              <div className="picker-empty">
                <p>{T.noImages}</p>
                <small>{T.noImagesHint}</small>
              </div>
            ) : (
              images.map((image) => (
                <div className="image-item" key={image.path} title={image.filename} onClick={() => onSelect(image.path)}>
                  <img src={imageSource(image)} alt={image.filename} loading="lazy" />
                  <div className="image-filename">{image.filename}</div>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// Attachments of quizzes/attach (#file-picker-modal).
export function FilePicker({
  files,
  onSelect,
  onClose,
}: {
  files: FileEntry[];
  onSelect: (path: string) => void;
  onClose: () => void;
}) {
  return (
    <div id="file-picker-modal" className="modal open">
      <div className="modal-content file-picker-content">
        <PickerHeader title={T.filePickerTitle} onClose={onClose} />
        <div id="file-picker-content">
          <div id="file-list" className="file-list">
            {files.length === 0 ? (
              <div className="picker-empty">
                <p>{T.noFiles}</p>
                <small>{T.noFilesHint}</small>
              </div>
            ) : (
              files.map((file) => (
                <div className="file-item" key={file.path} title={file.filename} onClick={() => onSelect(file.path)}>
                  <div className="file-item-info">
                    <span className="file-item-icon">📄</span>
                    <span className="file-item-name">{file.filename}</span>
                  </div>
                  <span className="file-item-size">{formatFileSize(file.size)}</span>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// A picture at full size. A click anywhere or Escape closes it.
export function FullscreenImage({ src, onClose }: { src: string; onClose: () => void }) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden'; // the page behind does not scroll
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = overflow;
    };
  }, [onClose]);

  return (
    <div id="image-fullscreen-modal" className="image-fullscreen-modal" onClick={onClose}>
      <span className="image-fullscreen-close">{T.close}</span>
      <img id="fullscreen-image" src={src} alt={T.fullSizeAlt} />
    </div>
  );
}
