import brandingStyles from './branding.css' with { type: 'css' };

export { brandingStyles };

const privateCaptchaSVG = `<svg viewBox="0 0 39.4 41.99" xml:space="preserve" xmlns="http://www.w3.org/2000/svg" class="pc-logo" preserveAspectRatio="xMidYMid meet">
<path d="M0 0v30.62l4.29 2.48V4.85h30.83v23.29l-15.41 8.9-6.83-3.94v-4.95l6.83 3.94 11.12-6.42V9.91H8.58v25.66l11.12 6.42 19.7-11.37V0Zm12.87 14.86h13.66v8.32l-6.83 3.94-6.83-3.94z" fill="currentColor"/>
</svg>`;

export function appendBranding(widget) {
    const spacer = document.createElement('div');
    spacer.className = 'pc-spacer';
    widget.appendChild(spacer);

    const info = document.createElement('div');
    info.className = 'pc-info';
    const template = document.createElement('template');
    template.innerHTML = privateCaptchaSVG;
    info.appendChild(template.content);
    const link = document.createElement('a');
    link.href = 'https://privatecaptcha.com';
    link.className = 'pc-link';
    link.rel = 'noopener nofollow';
    link.target = '_blank';
    link.appendChild(document.createTextNode('Private'));
    link.appendChild(document.createElement('br'));
    link.appendChild(document.createTextNode('Captcha'));
    info.appendChild(link);
    widget.appendChild(info);
}
