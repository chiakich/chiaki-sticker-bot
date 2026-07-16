import React, { useRef, useState } from 'react';
import './EmojiPicker.css';

// Curated set of emoji commonly used to tag Telegram stickers, grouped into
// tabs the way Telegram's own emoji keyboard does. Not the full Unicode set --
// free typing/pasting into the field still works for anything outside this list.
const CATEGORIES = [
  {
    key: 'smileys', icon: '😀', label: '表情',
    emojis: ['😀', '😃', '😄', '😁', '😆', '😅', '🤣', '😂', '🙂', '🙃',
      '😉', '😊', '😇', '🥰', '😍', '🤩', '😘', '😗', '😚', '😙',
      '😋', '😛', '😜', '🤪', '😝', '🤑', '🤗', '🤭', '🤫', '🤔',
      '🤐', '🤨', '😐', '😑', '😶', '😏', '😒', '🙄', '😬', '🤥',
      '😌', '😔', '😪', '🤤', '😴', '😷', '🤒', '🤕', '🤢', '🤮',
      '🥵', '🥶', '🥴', '😵', '🤯', '🤠', '🥳', '😎', '🤓', '🧐',
      '😕', '😟', '🙁', '😮', '😯', '😲', '😳', '🥺', '😦', '😧',
      '😨', '😰', '😥', '😢', '😭', '😱', '😖', '😣', '😞', '😓',
      '😩', '😫', '🥱', '😤', '😡', '😠', '🤬', '😈', '👿', '💀',
      '💩', '🤡', '👹', '👺', '👻', '👽', '🤖', '😺', '😸', '😹',
      '😻', '😼', '😽', '🙀', '😿', '😾'],
  },
  {
    key: 'gestures', icon: '👋', label: '手勢',
    emojis: ['👋', '🤚', '🖐️', '✋', '🖖', '👌', '🤌', '🤏', '✌️', '🤞',
      '🤟', '🤘', '🤙', '👈', '👉', '👆', '🖕', '👇', '☝️', '👍',
      '👎', '✊', '👊', '🤛', '🤜', '👏', '🙌', '👐', '🤲', '🙏',
      '💪', '🦾', '🫡'],
  },
  {
    key: 'animals', icon: '🐶', label: '動植物',
    emojis: ['🐶', '🐱', '🐭', '🐹', '🐰', '🦊', '🐻', '🐼', '🐨', '🐯',
      '🦁', '🐮', '🐷', '🐸', '🐵', '🐔', '🐧', '🐦', '🐤', '🦆',
      '🦉', '🦇', '🐺', '🐗', '🐴', '🦄', '🐝', '🐛', '🦋', '🐌',
      '🐞', '🐢', '🐍', '🦖', '🐙', '🦑', '🦀', '🐡', '🐠', '🐟',
      '🐬', '🐳', '🐄', '🐑', '🐘', '🦥', '🦦', '🐇', '🐁', '🐓',
      '🕊️', '🦅', '🦚', '🦜', '🌸', '🌹', '🌻', '🌼', '🌷', '🍀'],
  },
  {
    key: 'food', icon: '🍔', label: '食物',
    emojis: ['🍎', '🍊', '🍋', '🍌', '🍉', '🍇', '🍓', '🍒', '🍑', '🥭',
      '🍕', '🍔', '🍟', '🌭', '🍿', '🍩', '🍪', '🎂', '🍰', '🧁',
      '☕', '🍵', '🍺', '🍻', '🥤', '🍾'],
  },
  {
    key: 'activities', icon: '⚽', label: '活動',
    emojis: ['🎉', '🎊', '🎁', '🎈', '⚽', '🏀', '🎮', '🎵', '🎶',
      '🏆', '👑', '🎯'],
  },
  {
    key: 'symbols', icon: '❤️', label: '符號',
    emojis: ['❤️', '🧡', '💛', '💚', '💙', '💜', '🖤', '🤍', '🤎', '💔',
      '❣️', '💕', '💞', '💓', '💗', '💖', '💘', '💝', '💯', '💢',
      '💥', '💫', '💦', '💨', '💬', '🗨️', '🗯️', '💭', '💤',
      '⭐', '🌟', '✨', '⚡', '🔥', '🌈', '☀️', '🌙', '☁️', '❄️',
      '📌', '📎', '✅', '❌', '❓', '❗', '⚠️', '🚫', '🔔', '🔕', '💡', '🔑'],
  },
];

export function EmojiPickerPopup({ onSelect, onClear, onClose }) {
  const [activeCategory, setActiveCategory] = useState(0);
  const gridRef = useRef(null);

  function selectCategory(i) {
    setActiveCategory(i);
    if (gridRef.current) {
      gridRef.current.scrollTop = 0;
    }
  }

  return (
    <div className="EmojiPicker-Overlay" onClick={onClose}>
      <div className="EmojiPicker-Panel" onClick={(e) => e.stopPropagation()}>
        <div className="EmojiPicker-Header">
          <span>選擇 Emoji / Pick Emoji</span>
          <button type="button" className="EmojiPicker-Close" onClick={onClose}>✕</button>
        </div>
        <div className="EmojiPicker-Grid" ref={gridRef}>
          {CATEGORIES[activeCategory].emojis.map((e, i) => (
            <button type="button" key={i} className="EmojiPicker-Item"
              onClick={() => onSelect(e)}>
              {e}
            </button>
          ))}
        </div>
        <div className="EmojiPicker-Tabs">
          {CATEGORIES.map((cat, i) => (
            <button type="button" key={cat.key}
              className={'EmojiPicker-Tab' + (i === activeCategory ? ' active' : '')}
              onClick={() => selectCategory(i)}
              title={cat.label}>
              {cat.icon}
            </button>
          ))}
        </div>
        <div className="EmojiPicker-Footer">
          <button type="button" className="EmojiPicker-ClearBtn" onClick={onClear}>
            清空 / Clear
          </button>
          <button type="button" className="EmojiPicker-DoneBtn" onClick={onClose}>
            完成 / Done
          </button>
        </div>
      </div>
    </div>
  );
}
