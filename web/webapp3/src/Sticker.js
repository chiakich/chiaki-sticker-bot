import React, { forwardRef } from 'react';
// import axios from 'axios';
import Img from "react-cool-img";
import './StickerStyle.css'
import loading_gif from './loading.gif'



export const Sticker = forwardRef(({ id, faded, style, emoji, surl, is_video, onEmojiChange, ...props }, ref) => {

    return (
      <div className='Sticker-Div' ref={ref} style={style} {...props}>
          {is_video
            ? <video src={surl} autoPlay loop muted playsInline style={{width: '100%'}} />
            : <Img src={surl} placeholder={loading_gif} alt="Loading..."
                retry={{ count: 10, delay: 2, acc: false }}
              />
          }
        <br />
        <div>
          <label>{id}</label>
          <input type="text" value={emoji} size="6"
            onChange={(e) => onEmojiChange(id, e.target.value)}></input>
        </div>
      </div>
    );
});
