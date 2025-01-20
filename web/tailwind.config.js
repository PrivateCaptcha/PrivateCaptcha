/** @type {import('tailwindcss').Config} */
module.exports = {
    content: ['layouts/**/*.html'],
    theme: {
        extend: {},
    },
    plugins: [
        require('@tailwindcss/forms')({ strategy: 'class' }),
        require('@tailwindcss/typography')
    ],
    safelist: [
        'rotate-180'
    ]
}

