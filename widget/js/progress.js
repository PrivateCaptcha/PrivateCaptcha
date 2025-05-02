'use strict';

export class ProgressRing extends HTMLElement {
    constructor() {
        super();

        // NOTE: stroke is sized with regards to {size} variable below
        // not to the scaled (external) size of SVG (yes, it's a bit confusing)
        const stroke = this.getAttribute('stroke');
        const size = 100;
        const radius = size / 2;
        const normalizedRadius = radius - stroke / 2;
        this._circumference = normalizedRadius * 2 * Math.PI;

        // create shadow dom root
        this._root = this.attachShadow({ mode: 'open' });
        this._root.innerHTML = `
          <svg preserveAspectRatio="xMidYMid meet" viewBox="0 0 ${size} ${size}">
            <circle
               id="pie"
               style="stroke-dasharray: 0 ${this._circumference}"
               stroke-width="${normalizedRadius}"
               fill="transparent"
               r="${normalizedRadius / 2}"
               cx="${radius}"
               cy="${radius}"
            />
            <circle
               id="track"
               stroke-width="${stroke}"
               fill="transparent"
               r="${normalizedRadius}"
               cx="${radius}"
               cy="${radius}"
            />
             <circle
               id="progress"
               stroke-dasharray="${this._circumference} ${this._circumference}"
               style="stroke-dashoffset: ${this._circumference}"
               stroke-width="${stroke}"
               fill="transparent"
               r="${normalizedRadius}"
               cx="${radius}"
               cy="${radius}"
            />
          </svg>

          <style>
            svg {
                width: 2.25em;
                height: 2.25em;
            }
            #pie {
                stroke: var(--pie-color);
                transition: stroke-dasharray 0.35s;
                transform: rotate(-90deg);
                transform-origin: 50% 50%;
            }
            #progress {
                stroke: var(--accent-color);
                transition: stroke-dashoffset 0.35s;
                transform: rotate(-90deg);
                transform-origin: 50% 50%;
            }
            #track {
                stroke: var(--gray-color);
            }
          </style>
        `;
    }

    setProgress(percent) {
        const progress = (percent / 100 * this._circumference)
        const offset = this._circumference - progress;
        const circle = this._root.getElementById('progress');
        circle.style.strokeDashoffset = offset;

        const pie = this._root.getElementById('pie');
        pie.style.strokeDasharray = progress / 2 + ' ' + this._circumference;
    }

    static get observedAttributes() {
        return ['progress'];
    }

    attributeChangedCallback(name, oldValue, newValue) {
        if (name === 'progress') {
            this.setProgress(newValue);
        }
    }
}
