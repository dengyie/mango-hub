declare module "three" {
  export class Material {
    dispose(): void;
  }

  export class MeshBasicMaterial extends Material {
    constructor(parameters?: Record<string, unknown>);
  }

  export const DoubleSide: number;
}
