import { workspace, ExtensionContext, window } from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
} from 'vscode-languageclient/node';

let client: LanguageClient;

export async function activate(context: ExtensionContext): Promise<void> {
    const config = workspace.getConfiguration('bongo');
    let serverPath = config.get<string>('serverPath', '');
    if (!serverPath) {
        serverPath = 'bongo-ls';
    }

    const serverOptions: ServerOptions = {
        command: serverPath,
        args: [],
    };

    const clientOptions: LanguageClientOptions = {
        documentSelector: [{ scheme: 'file', language: 'bongo' }],
    };

    client = new LanguageClient('bongo', 'Bongo Language Server', serverOptions, clientOptions);
    context.subscriptions.push(client);
    try {
        await client.start();
    } catch (err) {
        window.showErrorMessage(
            `Bongo: failed to start bongo-ls ("${serverPath}"). ` +
            `Install it or set bongo.serverPath in settings. Error: ${err}`
        );
    }
}

export function deactivate(): Thenable<void> | undefined {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
