// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

const visualCursorMoveDelay = 170 * time.Millisecond

// moveVisualCursor is a presentation-only overlay. It never receives pointer
// events and failure to render it must never become the reason a browser action
// fails. The actual action remains CDP/DOM driven.
func moveVisualCursor(parent, pageCtx context.Context, selector string, pulse bool) error {
	selectorJSON, _ := json.Marshal(selector)
	expression := fmt.Sprintf(`(() => {
const selector=%s;
const target=document.querySelector(selector);
if(!target) return false;
target.scrollIntoView({block:'center',inline:'center'});
const rect=target.getBoundingClientRect();
if(rect.width<=0 || rect.height<=0) return false;
const styleId='__runtime_visual_cursor_style';
if(!document.getElementById(styleId)) {
  const style=document.createElement('style');
  style.id=styleId;
  style.textContent='@keyframes __runtimeCursorPulse{0%%{transform:scale(.45);opacity:.95}100%%{transform:scale(1.55);opacity:0}}';
  (document.head||document.documentElement).appendChild(style);
}
let host=document.getElementById('__runtime_visual_cursor');
if(!host) {
  host=document.createElement('div');
  host.id='__runtime_visual_cursor';
  host.setAttribute('aria-hidden','true');
  Object.assign(host.style,{position:'fixed',left:'0',top:'0',width:'28px',height:'28px',zIndex:'2147483647',pointerEvents:'none',userSelect:'none',opacity:'0',transition:'transform 150ms cubic-bezier(.2,.8,.2,1), opacity 90ms ease',willChange:'transform,opacity',transform:'translate3d('+(window.innerWidth/2)+'px,'+(window.innerHeight/2)+'px,0)'});
  const glow=document.createElement('div');
  Object.assign(glow.style,{position:'absolute',left:'-5px',top:'-5px',width:'30px',height:'30px',border:'2px solid rgba(56,189,248,.92)',borderRadius:'999px',boxShadow:'0 0 9px rgba(56,189,248,.95),0 0 22px rgba(14,165,233,.65),inset 0 0 7px rgba(125,211,252,.42)',background:'rgba(14,165,233,.08)'});
  const arrow=document.createElement('div');
  Object.assign(arrow.style,{position:'absolute',left:'4px',top:'3px',width:'19px',height:'24px',background:'linear-gradient(135deg,#f8fafc 0%%,#7dd3fc 48%%,#0284c7 100%%)',clipPath:'polygon(0 0,0 20px,5px 15px,9px 24px,13px 22px,9px 13px,18px 13px)',filter:'drop-shadow(0 0 4px rgba(125,211,252,1)) drop-shadow(0 0 10px rgba(14,165,233,.85))'});
  host.append(glow,arrow);
  document.documentElement.appendChild(host);
}
const x=Math.max(0,Math.min(window.innerWidth-1,rect.left+rect.width/2));
const y=Math.max(0,Math.min(window.innerHeight-1,rect.top+rect.height/2));
host.style.opacity='1';
void host.offsetWidth;
host.style.transform='translate3d('+(x-7)+'px,'+(y-5)+'px,0)';
if(%t) {
  const pulse=document.createElement('div');
  Object.assign(pulse.style,{position:'absolute',left:'-13px',top:'-13px',width:'46px',height:'46px',border:'3px solid rgba(56,189,248,.92)',borderRadius:'999px',boxShadow:'0 0 16px rgba(56,189,248,.9)',animation:'__runtimeCursorPulse 430ms ease-out forwards'});
  host.appendChild(pulse);
  setTimeout(()=>pulse.remove(),480);
}
return true;
})()`, selectorJSON, pulse)
	var rendered bool
	if err := runWithContext(parent, pageCtx, chromedp.Evaluate(expression, &rendered)); err != nil {
		return err
	}
	if rendered {
		return waitDuration(parent, visualCursorMoveDelay)
	}
	return nil
}
