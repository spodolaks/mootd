import { create } from 'zustand';

interface Toast {
  id: string;
  message: string;
  type: 'success' | 'error' | 'info';
}

interface UIState {
  toasts: Toast[];
  isLoading: boolean;
  showToast: (message: string, type?: Toast['type']) => void;
  dismissToast: (id: string) => void;
  setLoading: (loading: boolean) => void;
}

export const useUIStore = create<UIState>(set => ({
  toasts: [],
  isLoading: false,

  showToast: (message, type = 'info') => {
    const id = Date.now().toString();
    set(state => ({
      toasts: [...state.toasts, { id, message, type }],
    }));
    // Auto-dismiss after 3 seconds
    setTimeout(() => {
      set(state => ({
        toasts: state.toasts.filter(t => t.id !== id),
      }));
    }, 3000);
  },

  dismissToast: id => {
    set(state => ({
      toasts: state.toasts.filter(t => t.id !== id),
    }));
  },

  setLoading: isLoading => set({ isLoading }),
}));
