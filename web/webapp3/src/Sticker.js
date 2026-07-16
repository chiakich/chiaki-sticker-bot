import React, { forwardRef, useState } from 'react';
// import axios from 'axios';
import Img from "react-cool-img";
import './StickerStyle.css'
import loading_gif from './loading.gif'
import { EmojiPickerPopup } from './EmojiPicker'
import { EmojiAddIcon } from './EmojiAddIcon'



export const Sticker = forwardRef(({ id, faded, style, emoji, surl, onEmojiChange, ...props }, ref) => {
    const [pickerOpen, setPickerOpen] = useState(false);

    return (
      <div className='Sticker-Div' ref={ref} style={style} {...props}>
          <Img src={surl} placeholder={loading_gif} alt="Loading..."
            retry={{ count: 10, delay: 2, acc: false }}
          ></Img>
        <div className="Emoji-Row">
          <label>{id}</label>
          <input type="text" value={emoji}
            onChange={(e) => onEmojiChange?.(id, e.target.value)}></input>
          <button type="button" className="Emoji-Picker-Toggle"
            onClick={() => setPickerOpen(true)}><EmojiAddIcon /></button>
        </div>
        {pickerOpen &&
          <EmojiPickerPopup
            surl={surl}
            emoji={emoji}
            onSelect={(e) => onEmojiChange?.(id, (emoji || '') + e)}
            onClear={() => onEmojiChange?.(id, '')}
            onClose={() => setPickerOpen(false)}
          />
        }
      </div>
    );
});
