/** @type {import('tailwindcss').Config} */
module.exports = {
    content: ['layouts/**/*.html'],
    theme: {
        extend: {},
    },
    plugins: [
        require('@tailwindcss/forms')
    ],
    safelist: [
        'rotate-180'
    ]
}

