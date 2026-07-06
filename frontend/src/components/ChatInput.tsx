import {
  FormEvent,
  KeyboardEvent,
  type Ref,
  useEffect,
  useRef,
  useState,
} from "react";

type Props = {
  disabled: boolean;
  maxLength: number;
  body?: string;
  onBodyChange?: (body: string) => void;
  onAfterSend?: () => void;
  onSend: (body: string) => void;
  imageUploadEnabled?: boolean;
  onSendImage?: (file: File, caption: string) => void;
  textareaRef?: Ref<HTMLTextAreaElement>;
};

const ENTER_TO_SEND_KEY = "chat.enterToSend";

function loadEnterToSend(): boolean {
  try {
    // 未設定時は従来どおりEnter送信を既定にする。
    return window.localStorage.getItem(ENTER_TO_SEND_KEY) !== "false";
  } catch {
    return true;
  }
}

export default function ChatInput({
  disabled,
  maxLength,
  body: controlledBody,
  onBodyChange,
  onAfterSend,
  onSend,
  imageUploadEnabled = false,
  onSendImage,
  textareaRef,
}: Props) {
  const [uncontrolledBody, setUncontrolledBody] = useState("");
  const [enterToSend, setEnterToSend] = useState(loadEnterToSend);
  const [imageFile, setImageFile] = useState<File | null>(null);
  const [imagePreview, setImagePreview] = useState<string | null>(null);
  const [imageViewerOpen, setImageViewerOpen] = useState(false);
  const [dragActive, setDragActive] = useState(false);
  const imagePreviewRef = useRef<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const body = controlledBody ?? uncontrolledBody;
  const isVideoAttachment = imageFile?.type === "video/mp4";
  const charsLeft = maxLength - Array.from(body).length;
  const canAttachImage = imageUploadEnabled && !!onSendImage;
  const hasContent = body.trim() !== "" || imageFile !== null;

  useEffect(() => {
    try {
      window.localStorage.setItem(ENTER_TO_SEND_KEY, enterToSend ? "true" : "false");
    } catch {
      // localStorageが使えない環境では保存をスキップする。
    }
  }, [enterToSend]);

  // アンマウント時に残ったプレビューURLを解放する。
  useEffect(() => {
    return () => {
      if (imagePreviewRef.current) {
        URL.revokeObjectURL(imagePreviewRef.current);
      }
    };
  }, []);

  // 添付画像の設定。選択時にオブジェクトURLを同期生成して即座にサムネイル表示し、
  // 差し替え・解除のタイミングで古いURLを解放する。
  function setImage(file: File | null) {
    if (imagePreviewRef.current) {
      URL.revokeObjectURL(imagePreviewRef.current);
    }
    const url = file ? URL.createObjectURL(file) : null;
    imagePreviewRef.current = url;
    setImagePreview(url);
    setImageFile(file);
    setImageViewerOpen(false);
  }

  function clearImage() {
    setImage(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }

  function submitMessage() {
    if (disabled || !hasContent) {
      return;
    }
    const caption = body.trim();
    if (imageFile && onSendImage) {
      // 画像はリサイズ・アップロードを非同期で行うため即座に投稿欄をクリアする。
      onSendImage(imageFile, caption);
      clearImage();
      updateBody("");
      onAfterSend?.();
      return;
    }
    if (!caption) {
      return;
    }
    onSend(caption);
    updateBody("");
    onAfterSend?.();
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    submitMessage();
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    // Enter送信が無効なら通常の改行として扱う。
    if (!enterToSend) {
      return;
    }
    // Enterで送信、Shift+Enterで改行。IME変換確定のEnterは送信しない。
    // Safariは変換確定Enterで isComposing が正しく立たないため keyCode===229 も判定する。
    const composing = event.nativeEvent.isComposing || event.keyCode === 229;
    if (event.key === "Enter" && !event.shiftKey && !composing) {
      event.preventDefault();
      submitMessage();
    }
  }

  function updateBody(nextBody: string) {
    if (controlledBody !== undefined) {
      onBodyChange?.(nextBody);
      return;
    }
    setUncontrolledBody(nextBody);
  }

  function handlePickImage(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0] ?? null;
    setImage(file);
  }

  // 添付可能なファイル（静止画・GIF/APNG・MP4動画）か判定する。
  function isAttachable(file: File): boolean {
    return file.type.startsWith("image/") || file.type === "video/mp4";
  }

  // FileList から最初の添付可能ファイルを取り出す。
  function firstImageFile(files: FileList | null | undefined): File | null {
    if (!files) {
      return null;
    }
    for (const file of Array.from(files)) {
      if (isAttachable(file)) {
        return file;
      }
    }
    return null;
  }

  function handleDragOver(event: React.DragEvent) {
    if (!canAttachImage || disabled) {
      return;
    }
    // ファイルのドラッグ中だけドロップを受け付ける（テキスト選択のドラッグ等は無視）。
    if (event.dataTransfer.types?.includes("Files")) {
      event.preventDefault();
      setDragActive(true);
    }
  }

  function handleDragLeave(event: React.DragEvent) {
    // 子要素間の移動では解除しない。フォーム外に出たときだけ解除する。
    if (event.currentTarget.contains(event.relatedTarget as Node | null)) {
      return;
    }
    setDragActive(false);
  }

  function handleDrop(event: React.DragEvent) {
    if (!canAttachImage || disabled) {
      return;
    }
    const file = firstImageFile(event.dataTransfer.files);
    if (file) {
      // ブラウザが画像を開いてしまうのを防ぎ、添付として扱う。
      event.preventDefault();
      setImage(file);
    }
    setDragActive(false);
  }

  // 画像をペーストしたらファイル選択と同じく添付する（リサイズ・JPEG化は送信時に行う）。
  function handlePaste(event: React.ClipboardEvent<HTMLTextAreaElement>) {
    if (!canAttachImage || disabled) {
      return;
    }
    const items = event.clipboardData?.items;
    if (!items) {
      return;
    }
    for (const item of Array.from(items)) {
      if (item.kind === "file" && (item.type.startsWith("image/") || item.type === "video/mp4")) {
        const file = item.getAsFile();
        if (file) {
          event.preventDefault();
          setImage(file);
          return;
        }
      }
    }
  }

  return (
    <form
      className={`chatInput${dragActive ? " dragOver" : ""}`}
      onSubmit={submit}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {imageFile && imagePreview ? (
        <div className="imageAttachPreview">
          <button
            type="button"
            className="imageAttachPreviewButton"
            onClick={() => setImageViewerOpen(true)}
            aria-label="添付プレビューを拡大"
          >
            {isVideoAttachment ? (
              <video className="imageAttachThumb" src={imagePreview} muted loop autoPlay playsInline />
            ) : (
              <img className="imageAttachThumb" src={imagePreview} alt="添付プレビュー" />
            )}
          </button>
          <button
            type="button"
            className="imageAttachRemove"
            onClick={clearImage}
            aria-label="画像を取り消す"
          >
            ×
          </button>
        </div>
      ) : null}
      {imageViewerOpen && imagePreview ? (
        <button
          type="button"
          className="imageViewerOverlay"
          onClick={() => setImageViewerOpen(false)}
          aria-label="拡大画像を閉じる"
        >
          {isVideoAttachment ? (
            <video className="imageViewerImage" src={imagePreview} muted loop autoPlay playsInline controls />
          ) : (
            <img className="imageViewerImage" src={imagePreview} alt="拡大画像" />
          )}
        </button>
      ) : null}
      <textarea
        ref={textareaRef}
        value={body}
        maxLength={maxLength}
        rows={3}
        placeholder={
          enterToSend ? "メッセージ（Enterで送信、Shift+Enterで改行）" : "メッセージ"
        }
        onChange={(event) => updateBody(event.target.value)}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        disabled={disabled}
      />
      <div className="inputSide">
        <button type="submit" className="sendButton" disabled={disabled || !hasContent}>
          送信
        </button>
        {canAttachImage ? (
          <>
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*,video/mp4"
              className="srOnly"
              onChange={handlePickImage}
            />
            <button
              type="button"
              className="imageAttachButton"
              onClick={() => fileInputRef.current?.click()}
              disabled={disabled}
            >
              画像
            </button>
          </>
        ) : null}
        <span className={`charCount${charsLeft < 50 ? " limitWarning" : ""}`}>{charsLeft}</span>
        <label className="enterToSendToggle">
          <input
            type="checkbox"
            checked={enterToSend}
            onChange={(event) => setEnterToSend(event.target.checked)}
          />
          <span>Enterで送信</span>
        </label>
      </div>
    </form>
  );
}
