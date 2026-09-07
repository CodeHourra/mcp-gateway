import { createApp } from 'vue'
import App from './App.vue'
import './style.css'
import { installNativeBridge } from './native'

void installNativeBridge().then(() => createApp(App).mount('#app'))
