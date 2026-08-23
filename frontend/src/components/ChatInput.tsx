import {
  FormEvent,
  KeyboardEvent,
  type Ref,
  useEffect,
  useRef,
  useState,
} from "react";
import { isVideoFile } from "../lib/media";

type Props = {
  disabled: boolean;
  maxLength: number;
  body?: string;
  onBodyChange?: (body: string) => void;
  onAfterSend?: () => void;
  onSend: (body: string) => void;
  // 添付が可能か（チャンネル設定で許可、かつ処理中でない）。false なら添付ボタン等を出さない。
  canAttach?: boolean;
  // 画像添付ボタンを表示するか。モバイルの入力ドロワーでは別途「画像」FABを使うため false。
  showAttachButton?: boolean;
  // ファイル（画像/動画）が選ばれたら親へ渡す。親が投稿フォーム/編集画面を開く。
  onAttachFile?: (file: File) => void;
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
  canAttach = false,
  showAttachButton = true,
  onAttachFile,
  textareaRef,
}: Props) {
  const [uncontrolledBody, setUncontrolledBody] = useState("");
  const [enterToSend, setEnterToSend] = useState(loadEnterToSend);
  const [dragActive, setDragActive] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const body = controlledBody ?? uncontrolledBody;
  const charsLeft = maxLength - Array.from(body).length;
  const canAttachFile = canAttach && !!onAttachFile;
  const hasContent = body.trim() !== "";

  useEffect(() => {
    try {
      window.localStorage.setItem(ENTER_TO_SEND_KEY, enterToSend ? "true" : "false");
    } catch {
      // localStorageが使えない環境では保存をスキップする。
    }
  }, [enterToSend]);

  function submitMessage() {
    if (disabled || !hasContent) {
      return;
    }
    onSend(body.trim());
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

  // ファイル（画像/動画）を親へ渡す。親が投稿フォーム/動画編集を開く。
  function chooseFile(file: File) {
    if (!canAttachFile) {
      return;
    }
    onAttachFile?.(file);
    // 同じファイルを再選択してもトリガーできるよう入力をクリアする。
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }

  function handlePickImage(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0] ?? null;
    if (file) {
      chooseFile(file);
    }
  }

  // 添付可能なファイル（静止画・GIF/APNG・動画）か判定する。
  function isAttachable(file: File): boolean {
    return file.type.startsWith("image/") || isVideoFile(file);
  }

  // FileList から最初の添付可能ファイルを取り出す。
  function firstAttachable(files: FileList | null | undefined): File | null {
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
    if (!canAttachFile || disabled) {
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
    if (!canAttachFile || disabled) {
      return;
    }
    const file = firstAttachable(event.dataTransfer.files);
    if (file) {
      // ブラウザが画像を開いてしまうのを防ぎ、添付として扱う。
      event.preventDefault();
      chooseFile(file);
    }
    setDragActive(false);
  }

  // 画像/動画をペーストしたらファイル選択と同じく添付フローへ渡す。
  function handlePaste(event: React.ClipboardEvent<HTMLTextAreaElement>) {
    if (!canAttachFile || disabled) {
      return;
    }
    const items = event.clipboardData?.items;
    if (!items) {
      return;
    }
    for (const item of Array.from(items)) {
      if (item.kind === "file" && (item.type.startsWith("image/") || item.type.startsWith("video/"))) {
        const file = item.getAsFile();
        if (file) {
          event.preventDefault();
          chooseFile(file);
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
        {canAttachFile && showAttachButton ? (
          <>
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*,video/*"
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
