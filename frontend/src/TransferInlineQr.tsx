import {useEffect, useRef, useState} from 'react';
import QRCode from 'qrcode';
import {
  isCurrentQrRequest,
  normalizedQrPassphrase,
  qrGenerationRequest,
} from './inlineQrState';

const QR_OPTIONS = {
  width: 512,
  margin: 2,
  color: {
    dark: '#000000',
    light: '#ffffff',
  },
};

type Props = {
  passphrase: string;
  masked: boolean;
  onActivate: () => void;
  onError: (message: string) => void;
};

export function TransferInlineQr({passphrase, masked, onActivate, onError}: Props) {
  const normalized = normalizedQrPassphrase(passphrase);
  const generation = useRef(0);
  const generatedPassphrase = useRef('');
  const latestNormalizedPassphrase = useRef(normalized);
  latestNormalizedPassphrase.current = normalized;
  const onErrorRef = useRef(onError);
  const [dataUrl, setDataUrl] = useState('');
  const visibleDataUrl = generatedPassphrase.current === normalized ? dataUrl : '';

  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  useEffect(() => {
    const request = qrGenerationRequest(++generation.current, normalized);
    generatedPassphrase.current = '';
    setDataUrl('');
    if (!normalized) {
      return;
    }
    QRCode.toDataURL(normalized, QR_OPTIONS).then((next) => {
      if (isCurrentQrRequest(request, generation.current, latestNormalizedPassphrase.current)) {
        generatedPassphrase.current = request.passphrase;
        setDataUrl(next);
      }
    }).catch((error) => {
      if (isCurrentQrRequest(request, generation.current, latestNormalizedPassphrase.current)) {
        generatedPassphrase.current = '';
        setDataUrl('');
        onErrorRef.current(String(error));
      }
    });
  }, [normalized]);

  return (
    <button
      type="button"
      className={`transfer-inline-qr${masked ? ' masked' : ''}`}
      disabled={!visibleDataUrl}
      onClick={onActivate}
      aria-label="View passphrase QR code"
    >
      {visibleDataUrl && <img src={visibleDataUrl} alt="" />}
    </button>
  );
}
