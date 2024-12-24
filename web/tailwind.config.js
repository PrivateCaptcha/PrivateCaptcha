/** @type {import('tailwindcss').Config} */
module.exports = {
    content: ['layouts/**/*.html'],
    theme: {
        extend: {},
    },
    plugins: [
        require('@tailwindcss/forms'),
        require('@tailwindcss/typography')
    ],
    safelist: [
        'rotate-180'
    ]
}

