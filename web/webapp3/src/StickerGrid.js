import React from 'react';

export function StickerGrid({children, columns}) {
  return (
    <ul
      style={{
        maxWidth: '800px',
        display: 'grid',
        gridTemplateColumns: `repeat(${columns}, 1fr)`,
        gridGap: 10,
        padding: 0,
        margin: 0,
      }}
    >
      {children}
    </ul>
  );
}
