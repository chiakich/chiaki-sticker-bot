import React from 'react';
import './EmojiPicker.css';

// Curated set of emoji commonly used to tag Telegram stickers.
// Not the full Unicode set -- free typing/pasting into the field still works
// for anything outside this list.
const EMOJI_LIST = [
  '😀', '😃', '😄', '😁', '😆', '😅', '🤣', '😂', '🙂', '🙃',
  '😉', '😊', '😇', '🥰', '😍', '🤩', '😘', '😗', '😚', '😙',
  '😋', '😛', '😜', '🤪', '😝', '🤑', '🤗', '🤭', '🤫', '🤔',
  '🤐', '🤨', '😐', '😑', '😶', '😏', '😒', '🙄', '😬', '🤥',
  '😌', '😔', '😪', '🤤', '😴', '😷', '🤒', '🤕', '🤢', '🤮',
  '🥵', '🥶', '🥴', '😵', '🤯', '🤠', '🥳', '😎', '🤓', '🧐',
  '😕', '😟', '🙁', '😮', '😯', '😲', '😳', '🥺', '😦', '😧',
  '😨', '😰', '😥', '😢', '😭', '😱', '😖', '😣', '😞', '😓',
  '😩', '😫', '🥱', '😤', '😡', '😠', '🤬', '😈', '👿', '💀',
  '💩', '🤡', '👹', '👺', '👻', '👽', '🤖', '😺', '😸', '😹',
  '😻', '😼', '😽', '🙀', '😿', '😾',
  '❤️', '🧡', '💛', '💚', '💙', '💜', '🖤', '🤍', '🤎', '💔',
  '❣️', '💕', '💞', '💓', '💗', '💖', '💘', '💝', '💯', '💢',
  '💥', '💫', '💦', '💨', '🕳️', '💬', '👁️‍🗨️', '🗨️', '🗯️', '💭', '💤',
  '👋', '🤚', '🖐️', '✋', '🖖', '👌', '🤌', '🤏', '✌️', '🤞',
  '🤟', '🤘', '🤙', '👈', '👉', '👆', '🖕', '👇', '☝️', '👍',
  '👎', '✊', '👊', '🤛', '🤜', '👏', '🙌', '👐', '🤲', '🙏',
  '💪', '🦾', '🫡',
  '🐶', '🐱', '🐭', '🐹', '🐰', '🦊', '🐻', '🐼', '🐨', '🐯',
  '🦁', '🐮', '🐷', '🐸', '🐵', '🐔', '🐧', '🐦', '🐤', '🦆',
  '🦉', '🦇', '🐺', '🐗', '🐴', '🦄', '🐝', '🐛', '🦋', '🐌',
  '🐞', '🐢', '🐍', '🦖', '🐙', '🦑', '🦀', '🐡', '🐠', '🐟',
  '🐬', '🐳', '🐄', '🐑', '🐘', '🦥', '🦦', '🐇', '🐁', '🐓',
  '🕊️', '🦅', '🦚', '🦜', '🌸', '🌹', '🌻', '🌼', '🌷', '🍀',
  '🍎', '🍊', '🍋', '🍌', '🍉', '🍇', '🍓', '🍒', '🍑', '🥭',
  '🍕', '🍔', '🍟', '🌭', '🍿', '🍩', '🍪', '🎂', '🍰', '🧁',
  '☕', '🍵', '🍺', '🍻', '🥤', '🍾', '🎉', '🎊', '🎁', '🎈',
  '⭐', '🌟', '✨', '⚡', '🔥', '🌈', '☀️', '🌙', '☁️', '❄️',
  '⚽', '🏀', '🎮', '🎵', '🎶', '📌', '📎', '✅', '❌', '❓',
  '❗', '⚠️', '🚫', '🔔', '🔕', '💡', '🔑', '🎯', '🏆', '👑',
];

export function EmojiPickerPopup({ onSelect, onClear, onClose }) {
  return (
    <div className="EmojiPicker-Overlay" onClick={onClose}>
      <div className="EmojiPicker-Panel" onClick={(e) => e.stopPropagation()}>
        <div className="EmojiPicker-Header">
          <span>選擇 Emoji / Pick Emoji</span>
          <button type="button" className="EmojiPicker-Close" onClick={onClose}>✕</button>
        </div>
        <div className="EmojiPicker-Grid">
          {EMOJI_LIST.map((e, i) => (
            <button type="button" key={i} className="EmojiPicker-Item"
              onClick={() => onSelect(e)}>
              {e}
            </button>
          ))}
        </div>
        <button type="button" className="EmojiPicker-ClearBtn" onClick={onClear}>
          清空 / Clear
        </button>
      </div>
    </div>
  );
}
