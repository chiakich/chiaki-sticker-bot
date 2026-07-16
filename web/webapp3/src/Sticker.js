import React, { forwardRef, useEffect, useRef } from 'react';
// import axios from 'axios';
import Img from "react-cool-img";
import './StickerStyle.css'
import loading_gif from './loading.gif'


function usePlayWhenVisible(enabled) {
  const ref = useRef(null);

  useEffect(() => {
    const el = ref.current;
    if (!enabled || !el) {
      return;
    }
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) {
        el.play().catch(() => { }); // Rejects if the element is detached mid-play.
      } else {
        el.pause();
      }
    }, { rootMargin: '100px' });
    observer.observe(el);
    return () => observer.disconnect();
  }, [enabled]);

  return ref;
}

export const Sticker = forwardRef(({ id, faded, style, emoji, surl, is_video, onEmojiChange, ...props }, ref) => {
    const videoRef = usePlayWhenVisible(is_video);

    return (
      <div className='Sticker-Div' ref={ref} style={style} {...props}>
          {is_video
            ? <video ref={videoRef} src={surl} loop muted playsInline />
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
