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
                },
                'pcteal': {
                    50: "#E5FBFB",
                    100: "#CFF7F7",
                    200: "#9BEEEE",
                    300: "#6BE6E6",
                    400: "#3BDDDD",
                    500: "#20BBBB",
                    600: "#188B8B",
                    700: "#105B5B",
                    800: "#072929",
                    900: "#041616",
                    950: "#010909"
                },
                "pcslate": {
                    50: "#EBF4F4",
                    100: "#D8E9E9",
                    200: "#B4D5D5",
                    300: "#8DBEBE",
                    400: "#66A8A8",
                    500: "#4D8989",
                    600: "#376262",
                    700: "#223C3C",
                    800: "#162727",
                    900: "#0B1414",
                    950: "#060A0A"
                },
                "pclime": {
                    50: "#E0FFE0",
                    100: "#BDFFBD",
                    200: "#80FF80",
                    300: "#3DFF3D",
                    400: "#00FA00",
                    500: "#00BA00",
                    600: "#009400",
                    700: "#007000",
                    800: "#004D00",
                    900: "#002400",
                    950: "#001400"
                },
                "pcred": {
                    50: "#FDE2E2",
                    100: "#FBCACA",
                    200: "#F89191",
                    300: "#F45D5D",
                    400: "#F02828",
                    500: "#CF0E0E",
                    600: "#A70B0B",
                    700: "#7C0808",
                    800: "#510606",
                    900: "#2B0303",
                    950: "#130101"
                },
                "pcgray": {
                    50: "#F1F0F1",
                    100: "#E4E2E2",
                    200: "#CAC5C6",
                    300: "#B3ACAE",
                    400: "#9B9193",
                    500: "#82777A",
                    600: "#685E61",
                    700: "#51494B",
                    800: "#393335",
                    900: "#231F20",
                    950: "#151313"
                },
                'pcpalegreen': '#EFF1EF',
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

