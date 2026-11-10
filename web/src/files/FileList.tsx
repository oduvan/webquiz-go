import { canTransfer, csvTableUrl, formatDate, formatFileSize, type FileInfo, type FileType } from './api';
import { filesText as T } from './texts';

const smallButton = 'cursor-pointer rounded px-2.5 py-1 text-sm text-white no-underline ';
const blue = smallButton + 'bg-blue-600 hover:bg-blue-700';
const green = smallButton + 'bg-green-600 hover:bg-green-700';

export function FileList(props: {
  type: FileType;
  files: FileInfo[] | null; // null while loading
  search: string;
  selected: Set<string>;
  transferEnabled: boolean; // checkboxes show only while transfer is available
  onToggleTransfer: (type: FileType, name: string, checked: boolean) => void;
  onView: (type: FileType, name: string, mode: 'text' | 'table') => void;
  onEdit: (name: string) => void;
  onDownload: (type: FileType, name: string) => void;
}) {
  const { type, files, search } = props;

  if (files === null) {
    return (
      <div id={`${type}-files`} className="file-grid">
        <div className="loading p-6 text-center opacity-70">{T.loading[type]}</div>
      </div>
    );
  }
  if (files.length === 0) {
    return (
      <div id={`${type}-files`} className="file-grid">
        <div className="empty-state p-10 text-center opacity-70">
          <div className="icon text-4xl">{T.emptyIcon[type]}</div>
          <div>{T.empty[type]}</div>
        </div>
      </div>
    );
  }

  const term = search.toLowerCase().trim();
  return (
    <div id={`${type}-files`} className="file-grid flex flex-col gap-2">
      {files.map((file) => {
        const visible = !term || file.name.toLowerCase().includes(term);
        const key = `${type}/${file.name}`;
        return (
          <div
            key={file.name}
            className="file-item grid grid-cols-[1fr_auto] items-center gap-3 rounded border border-[var(--border)] p-3 max-md:grid-cols-1"
            style={visible ? undefined : { display: 'none' }}
            data-filename={file.name.toLowerCase()}
            data-name={file.name}
          >
            <div className="file-info min-w-0">
              <div className="file-name break-words font-bold">
                {props.transferEnabled && canTransfer(type, file.name) && (
                  <input
                    type="checkbox"
                    className="transfer-checkbox mr-2"
                    title={T.transferCheckboxTitle}
                    checked={props.selected.has(key)}
                    onChange={(event) => props.onToggleTransfer(type, file.name, event.target.checked)}
                  />
                )}
                {file.name}
              </div>
              <div className="file-meta text-sm opacity-70">
                {T.size}: {formatFileSize(file.size)} | {T.modified}: {formatDate(file.modified)}
              </div>
            </div>
            <div className="file-actions flex flex-wrap gap-2">
              {type === 'quizzes' && (
                <button type="button" className={blue} onClick={() => props.onEdit(file.name)}>
                  {T.edit}
                </button>
              )}
              {type === 'csv' && (
                <>
                  <button type="button" className={blue} onClick={() => props.onView(type, file.name, 'text')}>
                    {T.viewText}
                  </button>
                  <button type="button" className={blue} onClick={() => props.onView(type, file.name, 'table')}>
                    {T.viewTable}
                  </button>
                  <a className={blue + ' btn-new-window'} href={csvTableUrl(file.name)} target="_blank" rel="noopener" title={T.newWindowTitle}>
                    {T.newWindow}
                  </a>
                </>
              )}
              {type === 'logs' && (
                <button type="button" className={blue} onClick={() => props.onView(type, file.name, 'text')}>
                  {T.view}
                </button>
              )}
              <button type="button" className={green} onClick={() => props.onDownload(type, file.name)}>
                {T.download}
              </button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
