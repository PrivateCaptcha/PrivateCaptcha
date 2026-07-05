/** @type {import('tailwindcss').Config} */
module.exports = {
    content: ['layouts/**/*.html'],
    theme: {
        extend: {
            fontFamily: {
                sans: ['"DM Sans"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
                heading: ['Erode', 'serif'],
            },
            colors: {
                'pc-bg': '#faf8f5',
                'pc-black': '#000000',
                'pc-white': '#ffffff',
                'pc-green': {
                    DEFAULT: '#437540',
                    hover: '#2f522d',
                },
                'pc-success': '#009901',
                'pc-error': '#c33030',
                'pc-grey': {
                    950: '#0e0f0e',
                    800: '#464646',
                    600: '#686868',
                    400: '#A6A6A6',
                    250: '#bfbebc',
                    10: '#fffffc',
                },
                'pc-pastel': {
                    50: '#f5f7f0',
                    80: '#ecf0e7',
                    250: '#c5d1b7',
                },
                'pc-blue': {
                    DEFAULT: '#709ecc',
                    200: '#b3cae1',
                    500: '#5d7a93',
                    600: '#496175',
                },
                'pc-coral': {
                    DEFAULT: '#de6565',
                    50: '#fbf1f1',
                    100: '#f7e4e4',
                    300: '#e8a9a9',
                    500: '#dd5252',
                },
                "pcred": {
                    50: "#FDE2E2",
                    100: "#FBCACA",
                    400: "#F02828",
                    700: "#7C0808",
                },
            },
            borderRadius: {
                'pc': '2px',
                'pc-hover': '4px',
            },
            boxShadow: {
                'pc-hard': '2px 2px 0 #000',
            },
        },
    },
    plugins: [
        require('@tailwindcss/forms')({ strategy: 'class' }),
        require('@tailwindcss/typography')
    ],
    safelist: [
        'rotate-180'
    ]
}

