/** @type {import('tailwindcss').Config} */
module.exports = {
    content: ['layouts/**/*.html'],
    theme: {
        extend: {
            colors: {
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
                'pcpalegreen': '#EFF1EF',
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

